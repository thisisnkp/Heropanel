package broker_test

import (
	"context"
	"testing"

	brokerd "github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestSystemUserDelete(t *testing.T) {
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b, _ := newTestBroker(t, policy.Default(), fake)

	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "system_user.delete",
		Input:      mustJSON(t, map[string]string{"username": "hps1"}),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last, _ := fake.Last()
	if last.Path != "/usr/sbin/userdel" || !equalArgs(last.Args, []string{"hps1"}) {
		t.Fatalf("command = %+v", last)
	}
}

func TestSystemUserDeleteIdempotent(t *testing.T) {
	// Exit code 6 = no such user; must be treated as success.
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 6}}
	b, _ := newTestBroker(t, policy.Default(), fake)
	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "system_user.delete",
		Input:      mustJSON(t, map[string]string{"username": "hps1"}),
	}); err != nil {
		t.Fatalf("exit 6 should be success (idempotent), got %v", err)
	}
}

func TestSiteRemoveDirs(t *testing.T) {
	b, fs := newBrokerWithFS(t, &exec.FakeRunner{})
	_ = fs.WriteFile("/srv/heropanel/sites/1/public/index.html", []byte("x"), 0o644)

	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "site.remove_dirs",
		Input:      mustJSON(t, map[string]string{"root": "/srv/heropanel/sites/1"}),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := fs.Written("/srv/heropanel/sites/1/public/index.html"); ok {
		t.Fatal("directory tree should have been removed")
	}
}

func TestSiteRemoveDirsRejectsOutsidePolicy(t *testing.T) {
	b, _ := newBrokerWithFS(t, &exec.FakeRunner{})
	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "site.remove_dirs",
		Input:      mustJSON(t, map[string]string{"root": "/etc"}),
	})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden for out-of-policy root, got %v", err)
	}
}
