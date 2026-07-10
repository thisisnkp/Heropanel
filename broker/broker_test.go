package broker_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func newTestBroker(t *testing.T, pol policy.Policy, runner exec.Runner) (*broker.Broker, *[]audit.Entry) {
	t.Helper()
	var entries []audit.Entry
	chain := audit.NewChain(func(e audit.Entry) error {
		entries = append(entries, e)
		return nil
	})
	b := broker.New(broker.DefaultRegistry(), pol, chain, runner, nil)
	return b, &entries
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInvokeServiceRestartSuccess(t *testing.T) {
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b, entries := newTestBroker(t, policy.Default(), fake)

	resp, err := b.Invoke(context.Background(), broker.Request{
		Capability: "service.restart",
		Input:      mustJSON(t, map[string]string{"service": "mariadb"}),
		Actor:      capability.Actor{CorrelationID: "corr-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Data["restarted"] != true {
		t.Fatalf("unexpected response data: %+v", resp.Data)
	}

	// The exact argument array must be built — proving no shell interpolation.
	last, ok := fake.Last()
	if !ok || last.Path != "/usr/bin/systemctl" || !equalArgs(last.Args, []string{"restart", "mariadb"}) {
		t.Fatalf("command = %+v, want systemctl restart mariadb", last)
	}

	// Audit: intent then success, and the chain verifies.
	if len(*entries) != 2 ||
		(*entries)[0].Outcome != audit.OutcomeIntent ||
		(*entries)[1].Outcome != audit.OutcomeSuccess {
		t.Fatalf("unexpected audit trail: %+v", *entries)
	}
	if err := audit.Verify(*entries); err != nil {
		t.Fatalf("audit verify: %v", err)
	}
}

func TestInvokeDisabledCapabilityDenied(t *testing.T) {
	pol := policy.Default()
	pol.Enabled["service.restart"] = false
	fake := &exec.FakeRunner{}
	b, entries := newTestBroker(t, pol, fake)

	_, err := b.Invoke(context.Background(), broker.Request{
		Capability: "service.restart",
		Input:      mustJSON(t, map[string]string{"service": "mariadb"}),
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden, got %v (%s)", err, errx.KindOf(err))
	}
	if len(fake.Calls) != 0 {
		t.Fatal("no command may run when the capability is disabled")
	}
	if len(*entries) != 1 || (*entries)[0].Outcome != audit.OutcomeDenied {
		t.Fatalf("want a single denied audit entry, got %+v", *entries)
	}
}

func TestInvokeServiceNotAllowedForbidden(t *testing.T) {
	fake := &exec.FakeRunner{}
	b, entries := newTestBroker(t, policy.Default(), fake)

	_, err := b.Invoke(context.Background(), broker.Request{
		Capability: "service.restart",
		Input:      mustJSON(t, map[string]string{"service": "sshd"}),
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("no command may run for a disallowed service")
	}
	// intent was recorded, then failure (validation happened inside Execute).
	if len(*entries) != 2 || (*entries)[1].Outcome != audit.OutcomeFailure {
		t.Fatalf("want intent+failure audit, got %+v", *entries)
	}
}

func TestInvokeUnknownCapability(t *testing.T) {
	pol := policy.Default()
	pol.Enabled["nope.op"] = true // enabled but not registered
	fake := &exec.FakeRunner{}
	b, entries := newTestBroker(t, pol, fake)

	_, err := b.Invoke(context.Background(), broker.Request{Capability: "nope.op"})
	if !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
	if len(*entries) != 1 || (*entries)[0].Outcome != audit.OutcomeDenied {
		t.Fatalf("want a denied audit entry, got %+v", *entries)
	}
}

func TestInvokeUserCreateSuccessBuildsExactArgs(t *testing.T) {
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b, _ := newTestBroker(t, policy.Default(), fake)

	_, err := b.Invoke(context.Background(), broker.Request{
		Capability: "system_user.create",
		Input:      mustJSON(t, map[string]string{"username": "site1", "home": "/srv/heropanel/sites/1"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last, _ := fake.Last()
	want := []string{"--create-home", "--home-dir", "/srv/heropanel/sites/1", "--shell", "/usr/sbin/nologin", "--user-group", "site1"}
	if last.Path != "/usr/sbin/useradd" || !equalArgs(last.Args, want) {
		t.Fatalf("command = %+v, want useradd %v", last, want)
	}
}

func TestInvokeUserCreateRejectsBadUsername(t *testing.T) {
	fake := &exec.FakeRunner{}
	b, _ := newTestBroker(t, policy.Default(), fake)

	_, err := b.Invoke(context.Background(), broker.Request{
		Capability: "system_user.create",
		Input:      mustJSON(t, map[string]string{"username": "Bad Name", "home": "/srv/heropanel/sites/1"}),
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("no command may run for an invalid username")
	}
}

func TestInvokeUserCreateRejectsHomeOutsideRoot(t *testing.T) {
	fake := &exec.FakeRunner{}
	b, _ := newTestBroker(t, policy.Default(), fake)

	_, err := b.Invoke(context.Background(), broker.Request{
		Capability: "system_user.create",
		Input:      mustJSON(t, map[string]string{"username": "site1", "home": "/etc"}),
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden (path not allowed), got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("no command may run for a home outside allowed roots")
	}
}

func TestInvokeServiceRestartNonZeroExitIsUpstream(t *testing.T) {
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 1}}
	b, _ := newTestBroker(t, policy.Default(), fake)

	_, err := b.Invoke(context.Background(), broker.Request{
		Capability: "service.restart",
		Input:      mustJSON(t, map[string]string{"service": "mariadb"}),
	})
	if !errx.IsKind(err, errx.KindUpstream) {
		t.Fatalf("want upstream error on non-zero exit, got %v", err)
	}
}
