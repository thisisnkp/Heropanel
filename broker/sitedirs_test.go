package broker_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestInvokeSiteCreateDirs(t *testing.T) {
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b, _ := newTestBroker(t, policy.Default(), fake)

	_, err := b.Invoke(context.Background(), broker.Request{
		Capability: "site.create_dirs",
		Input:      mustJSON(t, map[string]string{"username": "hps1", "root": "/srv/heropanel/sites/1"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Five directories: root, public, logs, tmp, sessions.
	if len(fake.Calls) != 5 {
		t.Fatalf("expected 5 install commands, got %d", len(fake.Calls))
	}

	// Root dir, 0750, owned by the site user.
	if got := fake.Calls[0]; got.Path != "/usr/bin/install" ||
		!equalArgs(got.Args, []string{"-d", "-m", "0750", "-o", "hps1", "-g", "hps1", "/srv/heropanel/sites/1"}) {
		t.Fatalf("root command = %+v", got)
	}
	// public dir, 0750.
	if got := fake.Calls[1]; !equalArgs(got.Args, []string{"-d", "-m", "0750", "-o", "hps1", "-g", "hps1", "/srv/heropanel/sites/1/public"}) {
		t.Fatalf("public command = %+v", got)
	}
	// tmp dir must be private (0700).
	if got := fake.Calls[3]; !equalArgs(got.Args, []string{"-d", "-m", "0700", "-o", "hps1", "-g", "hps1", "/srv/heropanel/sites/1/tmp"}) {
		t.Fatalf("tmp command = %+v", got)
	}
}

func TestInvokeSiteCreateDirsRejectsRootOutsidePolicy(t *testing.T) {
	fake := &exec.FakeRunner{}
	b, _ := newTestBroker(t, policy.Default(), fake)

	_, err := b.Invoke(context.Background(), broker.Request{
		Capability: "site.create_dirs",
		Input:      mustJSON(t, map[string]string{"username": "hps1", "root": "/etc"}),
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden for out-of-policy root, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("no directory should be created for a disallowed root")
	}
}
