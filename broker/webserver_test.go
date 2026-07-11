package broker_test

import (
	"context"
	"strings"
	"testing"

	brokerd "github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func newBrokerWithFS(t *testing.T, runner exec.Runner) (*brokerd.Broker, *fsys.Fake) {
	t.Helper()
	fake := fsys.NewFake()
	b := brokerd.New(brokerd.DefaultRegistry(), policy.Default(), audit.NewChain(nil), runner, nil)
	b.SetFS(fake)
	return b, fake
}

func TestWebserverApplyWritesTestsReloads(t *testing.T) {
	var ran []string
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		ran = append(ran, c.Path+" "+strings.Join(c.Args, " "))
		return exec.Result{ExitCode: 0}, nil
	}}
	b, fs := newBrokerWithFS(t, runner)

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "webserver.apply",
		Input: mustJSON(t, map[string]any{
			"vhosts":   []map[string]string{{"name": "hps1", "config": "docRoot /srv/heropanel/sites/1/public\n"}},
			"listener": "listener HeroPanelHTTP {\n  address *:80\n}\n",
		}),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	// vhost config + listener config written.
	if got, ok := fs.Written("/usr/local/lsws/conf/vhosts/hps1/vhconf.conf"); !ok || !strings.Contains(got, "docRoot") {
		t.Fatalf("vhost config not written correctly: %q", got)
	}
	if _, ok := fs.Written("/usr/local/lsws/conf/heropanel.conf"); !ok {
		t.Fatal("listener config not written")
	}

	// Config tested, then reloaded, in order.
	if len(ran) != 2 ||
		!strings.Contains(ran[0], "lshttpd -t") ||
		!strings.Contains(ran[1], "lswsctrl reload") {
		t.Fatalf("unexpected commands: %v", ran)
	}
}

func TestWebserverApplyRollsBackOnFailedTest(t *testing.T) {
	// The config test returns non-zero → the capability must roll back.
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if strings.Contains(strings.Join(c.Args, " "), "-t") {
			return exec.Result{ExitCode: 1}, nil // test fails
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	b, fs := newBrokerWithFS(t, runner)

	// Seed a prior listener config to verify it is restored.
	_ = fs.WriteFile("/usr/local/lsws/conf/heropanel.conf", []byte("PRIOR"), 0o644)

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "webserver.apply",
		Input: mustJSON(t, map[string]any{
			"vhosts":   []map[string]string{{"name": "hps1", "config": "NEW"}},
			"listener": "NEW-LISTENER",
		}),
	})
	if !errx.IsKind(err, errx.KindUpstream) {
		t.Fatalf("want upstream error on failed test, got %v", err)
	}
	// Prior listener content restored.
	if got, _ := fs.Written("/usr/local/lsws/conf/heropanel.conf"); got != "PRIOR" {
		t.Fatalf("listener not rolled back, got %q", got)
	}
	// Newly-created vhost file (had no prior) removed.
	if _, ok := fs.Written("/usr/local/lsws/conf/vhosts/hps1/vhconf.conf"); ok {
		t.Fatal("new vhost file should have been removed on rollback")
	}
}

func TestWebserverApplyRejectsBadVhostName(t *testing.T) {
	b, _ := newBrokerWithFS(t, &exec.FakeRunner{})
	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "webserver.apply",
		Input: mustJSON(t, map[string]any{
			"vhosts":   []map[string]string{{"name": "../evil", "config": "x"}},
			"listener": "x",
		}),
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error for bad vhost name, got %v", err)
	}
}
