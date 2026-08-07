package site_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeRegistry stands in for the parked-domain/DNS-zone registry
// (internal/domain's Classify), recording the call and returning a canned
// status or error.
type fakeRegistry struct {
	status            string
	err               error
	calls             int
	gotOwner, gotSite int64
	gotFQDN           string
}

func (f *fakeRegistry) Classify(_ context.Context, ownerID, siteID int64, fqdn string) (string, error) {
	f.calls++
	f.gotOwner, f.gotSite, f.gotFQDN = ownerID, siteID, fqdn
	return f.status, f.err
}

// With no registry wired, a new site's DNSStatus stays empty — the same
// degrade-gracefully behavior every other optional dep has.
func TestCreateWithoutRegistryLeavesDNSStatusEmpty(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	out, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.DNSStatus != "" {
		t.Fatalf("DNSStatus = %q, want empty with no registry wired", out.DNSStatus)
	}
}

// A registry reporting "verified" (a free/parked domain, or a subdomain of one)
// is classified once, with the site's real owner/id/domain, and the result
// lands on the created site's view.
func TestCreateClassifiesDomainAsVerified(t *testing.T) {
	store, _ := newStore(t)
	reg := &fakeRegistry{status: "verified"}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}, Registry: reg})

	out, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.DNSStatus != "verified" {
		t.Fatalf("DNSStatus = %q, want verified", out.DNSStatus)
	}
	if reg.calls != 1 {
		t.Fatalf("Classify called %d times, want 1", reg.calls)
	}
	if reg.gotOwner != 1 || reg.gotFQDN != "acme.example.com" {
		t.Fatalf("Classify args = owner=%d fqdn=%q, want owner=1 fqdn=acme.example.com", reg.gotOwner, reg.gotFQDN)
	}
	if reg.gotSite == 0 {
		t.Fatal("Classify should receive the new site's real internal id, got 0")
	}
}

// An arbitrary domain with no proven ownership still creates the site — DNS
// status is advisory, never a gate — but is reported unverified.
func TestCreateClassifiesUnknownDomainAsUnverified(t *testing.T) {
	store, _ := newStore(t)
	reg := &fakeRegistry{status: "unverified"}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}, Registry: reg})

	out, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.Status != site.StatusActive {
		t.Fatalf("an unverified domain must not block site creation, got status=%q", out.Status)
	}
	if out.DNSStatus != "unverified" {
		t.Fatalf("DNSStatus = %q, want unverified", out.DNSStatus)
	}
}

// A registry error never fails site creation — DNS classification is
// best-effort bookkeeping, not a provisioning step.
func TestCreateSurvivesRegistryError(t *testing.T) {
	store, _ := newStore(t)
	reg := &fakeRegistry{err: errx.Internal(nil)}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}, Registry: reg})

	out, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("a registry error must not fail site creation: %v", err)
	}
	if out.Status != site.StatusActive {
		t.Fatalf("status = %q, want active", out.Status)
	}
	if out.DNSStatus != "" {
		t.Fatalf("DNSStatus = %q, want empty when classification errored", out.DNSStatus)
	}
}
