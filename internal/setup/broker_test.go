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
		Webserver: WebserverNginx, DBEngine: DBEnginePostgreSQL,
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
	// nginx + postgresql + bind + postfix + dovecot, in order.
	want := []string{"nginx", "postgresql", "bind", "postfix", "dovecot"}
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

// A minimal selection (no DNS, no mail) sends just the webserver + engine.
func TestSelectionComponents_Minimal(t *testing.T) {
	comps := Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB}.Components()
	if len(comps) != 2 || comps[0] != "openlitespeed" || comps[1] != "mariadb" {
		t.Fatalf("components = %v", comps)
	}
}
