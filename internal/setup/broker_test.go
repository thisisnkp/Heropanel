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
	if len(inv.calls) != 1 || inv.calls[0].cap != ProvisionCapability {
		t.Fatalf("expected one call to %s, got %+v", ProvisionCapability, inv.calls)
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
