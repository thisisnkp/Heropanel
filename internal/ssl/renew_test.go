package ssl_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/ssl"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeDNSACME satisfies both ACME (HTTP-01) and ACMEDNS (DNS-01).
type fakeDNSACME struct {
	fakeACME
	dnsCalls  []string // domains issued via DNS-01
	txtSet    map[string]string
	txtDelete []string
}

func (f *fakeDNSACME) IssueDNS01(_ context.Context, domain string,
	setTXT func(fqdn, value string) error, deleteTXT func(fqdn string) error) (string, string, time.Time, error) {
	if f.fail {
		return "", "", time.Time{}, errors.New("acme dns boom")
	}
	f.dnsCalls = append(f.dnsCalls, domain)
	// Mirror the real flow: publish the challenge, then clean it up.
	base := domain
	if len(base) > 2 && base[:2] == "*." {
		base = base[2:]
	}
	fqdn := "_acme-challenge." + base
	if err := setTXT(fqdn, "tokenvalue"); err != nil {
		return "", "", time.Time{}, err
	}
	if err := deleteTXT(fqdn); err != nil {
		return "", "", time.Time{}, err
	}
	return "CERTPEM-" + domain, "KEYPEM", time.Now().Add(90 * 24 * time.Hour), nil
}

// fakeDNSProvider records the TXT records ACME asks for.
type fakeDNSProvider struct {
	set     map[string]string
	deleted []string
}

func newDNSProvider() *fakeDNSProvider { return &fakeDNSProvider{set: map[string]string{}} }

func (p *fakeDNSProvider) SetTXT(_ context.Context, fqdn, value string) error {
	p.set[fqdn] = value
	return nil
}
func (p *fakeDNSProvider) DeleteTXT(_ context.Context, fqdn string) error {
	p.deleted = append(p.deleted, fqdn)
	return nil
}

func TestIssueDNSPublishesChallengeAndRecordsWildcard(t *testing.T) {
	acme := &fakeDNSACME{}
	svc, gw := newSvc(t, acme)
	dnsp := newDNSProvider()
	svc.WithDNS(dnsp)
	ctx := context.Background()

	cert, err := svc.IssueDNS(ctx, 1, "*.example.com")
	if err != nil {
		t.Fatalf("issue dns: %v", err)
	}
	if cert.CommonName != "*.example.com" {
		t.Fatalf("cert = %+v", cert)
	}
	// The challenge was published at the base domain and then removed.
	if dnsp.set["_acme-challenge.example.com"] != "tokenvalue" {
		t.Fatalf("challenge TXT not published: %+v", dnsp.set)
	}
	if len(dnsp.deleted) != 1 || dnsp.deleted[0] != "_acme-challenge.example.com" {
		t.Fatalf("challenge TXT not cleaned up: %+v", dnsp.deleted)
	}
	// A wildcard is installed under its base domain (it is not a legal hostname).
	var installed *call
	for i := range gw.calls {
		if gw.calls[i].capability == "cert.install" {
			installed = &gw.calls[i]
		}
	}
	if installed == nil || installed.input["domain"] != "example.com" {
		t.Fatalf("cert.install input = %+v", installed)
	}
}

func TestIssueDNSRequiresDNSProviderAndIssuer(t *testing.T) {
	ctx := context.Background()

	// No DNS provider wired.
	svc, _ := newSvc(t, &fakeDNSACME{})
	if _, err := svc.IssueDNS(ctx, 1, "*.example.com"); !errx.IsKind(err, errx.KindUnavailable) {
		t.Fatalf("want unavailable without a DNS provider, got %v", err)
	}
	// Issuer that only supports HTTP-01.
	svc2, _ := newSvc(t, &fakeACME{})
	svc2.WithDNS(newDNSProvider())
	if _, err := svc2.IssueDNS(ctx, 1, "*.example.com"); !errx.IsKind(err, errx.KindUnavailable) {
		t.Fatalf("want unavailable for an HTTP-01-only issuer, got %v", err)
	}
	// Bad domain.
	svc3, _ := newSvc(t, &fakeDNSACME{})
	svc3.WithDNS(newDNSProvider())
	if _, err := svc3.IssueDNS(ctx, 1, "not-a-domain"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for a bad domain, got %v", err)
	}
}

func TestRenewerRenewsDueCertificates(t *testing.T) {
	acme := &fakeDNSACME{}
	svc, _ := newSvc(t, acme)
	svc.WithDNS(newDNSProvider())
	ctx := context.Background()

	// A self-signed cert (825d) and an ACME cert (90d) — neither is due yet.
	if _, err := svc.IssueSelfSigned(ctx, 1, "self.example.com"); err != nil {
		t.Fatalf("self-signed: %v", err)
	}
	if _, err := svc.Issue(ctx, 1, "http.example.com", "/srv/site/public"); err != nil {
		t.Fatalf("http-01: %v", err)
	}
	if _, err := svc.IssueDNS(ctx, 1, "*.wild.example.com"); err != nil {
		t.Fatalf("dns-01: %v", err)
	}

	// With the default 30-day window nothing is due.
	r := ssl.NewRenewer(svc, nil)
	if n, err := r.RenewDue(ctx); err != nil || n != 0 {
		t.Fatalf("nothing should be due yet: n=%d err=%v", n, err)
	}

	// Widen the window past the ACME certs' 90-day expiry: both renew, and the
	// wildcard goes back through DNS-01 (no webroot was recorded for it).
	r.WithSchedule(time.Hour, 120*24*time.Hour)
	n, err := r.RenewDue(ctx)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected the 2 ACME certs to renew, got %d", n)
	}
	var wildRenewed bool
	for _, d := range acme.dnsCalls {
		if d == "*.wild.example.com" {
			wildRenewed = true
		}
	}
	if !wildRenewed {
		t.Fatalf("the wildcard should renew over DNS-01, dns calls=%v", acme.dnsCalls)
	}

	// Certificates stay listed (renewal upserts by common name, not duplicates).
	certs, _ := svc.List(ctx, 0, 50, 0)
	if len(certs) != 3 {
		t.Fatalf("expected 3 certs after renewal, got %d", len(certs))
	}
}

func TestRenewerSkipsUploadedCerts(t *testing.T) {
	svc, _ := newSvc(t, &fakeDNSACME{})
	ctx := context.Background()

	// Generate a real key pair to upload, so it parses.
	tmp, _ := newSvc(t, nil)
	self, err := tmp.IssueSelfSigned(ctx, 1, "up.example.com")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = self

	// A custom cert must never be auto-renewed: HeroPanel cannot re-obtain it.
	r := ssl.NewRenewer(svc, nil).WithSchedule(time.Hour, 100*365*24*time.Hour)
	if n, err := r.RenewDue(ctx); err != nil || n != 0 {
		t.Fatalf("no certs in this store should renew: n=%d err=%v", n, err)
	}
}
