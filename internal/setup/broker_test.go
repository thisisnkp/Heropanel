package setup

import (
	"context"
	"errors"
	"testing"
)

type fakeInvoker struct {
	calls []struct {
		cap   string
		input any
	}
	err error
}

func (f *fakeInvoker) Invoke(_ context.Context, cap string, input any) (map[string]any, error) {
	f.calls = append(f.calls, struct {
		cap   string
		input any
	}{cap, input})
	return map[string]any{}, f.err
}

func TestBrokerProvisioner_SendsComponents(t *testing.T) {
	inv := &fakeInvoker{}
	p := NewBrokerProvisioner(inv)
	plan := BuildPlan(Selection{
		Webserver: WebserverLiteSpeed, DBEngine: DBEngineMariaDB,
		ManageDNS: true, CreateMail: true,
	})
	if err := p.Provision(context.Background(), plan); err != nil {
		t.Fatalf("provision: %v", err)
	}
	// Two calls: the packages, then maldet — which has no package and so cannot
	// travel in the components list.
	if len(inv.calls) != 2 || inv.calls[0].cap != ProvisionCapability {
		t.Fatalf("expected %s then %s, got %+v", ProvisionCapability, MaldetInstallCapability, inv.calls)
	}
	if inv.calls[1].cap != MaldetInstallCapability {
		t.Fatalf("maldet was not installed: %+v", inv.calls)
	}
	payload, ok := inv.calls[0].input.(map[string]any)
	if !ok {
		t.Fatalf("input not a map: %T", inv.calls[0].input)
	}
	comps, ok := payload["components"].([]string)
	if !ok {
		t.Fatalf("components not []string: %T", payload["components"])
	}
	// The webserver and MariaDB, then the always-on baseline, then the optional
	// modules — in install order.
	want := []string{
		"litespeed_enterprise", "mariadb",
		"phpmyadmin", "clamav", "fail2ban", "modsecurity", "nftables",
		"bind", "postfix", "dovecot",
	}
	if len(comps) != len(want) {
		t.Fatalf("components = %v, want %v", comps, want)
	}
	for i := range want {
		if comps[i] != want[i] {
			t.Fatalf("components[%d] = %q, want %q (all: %v)", i, comps[i], want[i], comps)
		}
	}
}

func TestBrokerProvisioner_PropagatesError(t *testing.T) {
	inv := &fakeInvoker{err: errors.New("broker down")}
	p := NewBrokerProvisioner(inv)
	err := p.Provision(context.Background(), BuildPlan(Selection{
		Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB,
	}))
	if err == nil {
		t.Fatal("expected the broker error to propagate")
	}
}

// Even the most minimal selection — no DNS, no mail — still carries the
// baseline. That is the point of the baseline: there is no way to answer the
// wizard that produces a host without a firewall, a WAF or a malware scanner.
func TestSelectionComponents_MinimalStillCarriesBaseline(t *testing.T) {
	comps := Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB}.Components()
	want := []string{"openlitespeed", "mariadb", "phpmyadmin", "clamav", "fail2ban", "modsecurity", "nftables"}
	if len(comps) != len(want) {
		t.Fatalf("components = %v, want %v", comps, want)
	}
	for i := range want {
		if comps[i] != want[i] {
			t.Fatalf("components[%d] = %q, want %q (all: %v)", i, comps[i], want[i], comps)
		}
	}
}

// maldet failing does not fail setup.
//
// Every other baseline component comes from the host's own repositories;
// maldet comes from a third party over the public internet. rfxn.com being
// briefly unreachable must not hold a whole first-run install hostage — the
// operator gets a working panel with maldet missing and a one-click Install on
// the malware screen, rather than a wizard they cannot get past.
func TestBrokerProvisioner_MaldetFailureDoesNotFailSetup(t *testing.T) {
	inv := &failOnInvoker{fail: MaldetInstallCapability}
	p := NewBrokerProvisioner(inv)
	plan := BuildPlan(Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB})

	if err := p.Provision(context.Background(), plan); err != nil {
		t.Fatalf("setup failed because maldet could not be installed: %v", err)
	}
	if inv.tried != MaldetInstallCapability {
		t.Errorf("maldet was never attempted: %q", inv.tried)
	}
}

// ...but a package failure still does. Those come from repositories the host is
// already configured to trust, so a failure there means the stack is not there.
func TestBrokerProvisioner_PackageFailureStillFailsSetup(t *testing.T) {
	inv := &failOnInvoker{fail: ProvisionCapability}
	p := NewBrokerProvisioner(inv)
	plan := BuildPlan(Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB})

	if err := p.Provision(context.Background(), plan); err == nil {
		t.Fatal("setup reported success despite the packages failing")
	}
}

// failOnInvoker fails one named capability and records the last one attempted.
type failOnInvoker struct {
	fail  string
	tried string
}

func (f *failOnInvoker) Invoke(_ context.Context, cap string, _ any) (map[string]any, error) {
	f.tried = cap
	if cap == f.fail {
		return nil, errors.New(cap + " unavailable")
	}
	return map[string]any{}, nil
}
