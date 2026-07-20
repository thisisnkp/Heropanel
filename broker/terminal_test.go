package broker_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// A terminal is the most powerful thing the broker hands out, so every one of
// its refusals is asserted here. These all decide *before* a PTY is allocated,
// which is why they run on any platform — actually spawning the shell is what
// the Linux e2e (run-terminal.sh) proves.

const termRoot = "/srv/heropanel/sites/1"

func lastOutcome(entries *[]audit.Entry) audit.Outcome {
	if len(*entries) == 0 {
		return ""
	}
	return (*entries)[len(*entries)-1].Outcome
}

func TestTerminalDeniedWhenPolicyDisabled(t *testing.T) {
	pol := policy.Default()
	pol.Enabled["terminal.open"] = false
	b, entries := newTestBroker(t, pol, &exec.FakeRunner{})

	_, err := b.OpenTerminal(context.Background(), broker.TerminalRequest{
		Username: "hps1", Root: termRoot,
		Actor: capability.Actor{CorrelationID: "c1"},
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden when policy disables terminals, got %v", err)
	}
	if got := lastOutcome(entries); got != audit.OutcomeDenied {
		t.Errorf("audit outcome = %q, want denied", got)
	}
}

func TestTerminalRefusesRoot(t *testing.T) {
	b, entries := newTestBroker(t, policy.Default(), &exec.FakeRunner{})

	_, err := b.OpenTerminal(context.Background(), broker.TerminalRequest{
		Username: "root", Root: termRoot,
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("a root terminal must be refused, got %v", err)
	}
	if got := lastOutcome(entries); got != audit.OutcomeDenied {
		t.Errorf("audit outcome = %q, want denied", got)
	}
}

func TestTerminalRejectsInvalidUsername(t *testing.T) {
	b, _ := newTestBroker(t, policy.Default(), &exec.FakeRunner{})

	for _, name := range []string{"", "has space", "Upper", "-leading", "a;rm -rf /"} {
		if _, err := b.OpenTerminal(context.Background(), broker.TerminalRequest{
			Username: name, Root: termRoot,
		}); err == nil {
			t.Errorf("username %q should be rejected", name)
		}
	}
}

func TestTerminalRejectsRootOutsidePolicy(t *testing.T) {
	b, entries := newTestBroker(t, policy.Default(), &exec.FakeRunner{})

	_, err := b.OpenTerminal(context.Background(), broker.TerminalRequest{
		Username: "hps1", Root: "/etc",
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("a home outside the policy roots must be forbidden, got %v", err)
	}
	if got := lastOutcome(entries); got != audit.OutcomeDenied {
		t.Errorf("audit outcome = %q, want denied", got)
	}
}

// The starting directory is clamped the same way file operations are: a cwd of
// "../../.." resolves back to the site root, never above it. If the clamp were
// broken this would be forbidden or escape; either way it would not reach the
// PTY step, so we assert the request gets past authorization to that point.
func TestTerminalClampsWorkingDirectory(t *testing.T) {
	b, entries := newTestBroker(t, policy.Default(), &exec.FakeRunner{})

	_, err := b.OpenTerminal(context.Background(), broker.TerminalRequest{
		Username: "hps1", Root: termRoot, Cwd: "../../../../etc",
	})
	// Authorization succeeded (the traversal was clamped under the root), so the
	// only thing that can fail now is allocating the PTY itself — which is
	// exactly what happens on a non-Linux dev machine or a test container.
	if errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("a clamped cwd must not be forbidden: %v", err)
	}
	// Intent is recorded before anything privileged happens, always.
	var sawIntent bool
	for _, e := range *entries {
		if e.Outcome == audit.OutcomeIntent && e.Capability == broker.CapTerminalOpen {
			sawIntent = true
		}
	}
	if !sawIntent {
		t.Error("an intent entry must be recorded before the session is started")
	}
}
