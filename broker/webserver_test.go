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

	// Running server: a graceful reload applies the config; the unreliable-while-
	// running `-t` gate is skipped.
	if len(ran) != 1 || !strings.Contains(ran[0], "lswsctrl reload") {
		t.Fatalf("expected a single graceful reload, got: %v", ran)
	}
}

func TestWebserverApplyRollsBackWhenStoppedAndInvalid(t *testing.T) {
	// Server not running (reload fails) AND the config is invalid (-t fails) →
	// the capability must roll back.
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if strings.Contains(strings.Join(c.Args, " "), "reload") {
			return exec.Result{ExitCode: 2}, nil // litespeed not running
		}
		if strings.Contains(strings.Join(c.Args, " "), "-t") {
			return exec.Result{ExitCode: 1}, nil // config invalid
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

func TestWebserverApplyStoppedButValid(t *testing.T) {
	// Server not running (reload fails) but the config is valid (-t = 0) →
	// success without rollback; the config stays written for the next start.
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if strings.Contains(strings.Join(c.Args, " "), "reload") {
			return exec.Result{ExitCode: 2}, nil // litespeed not running
		}
		return exec.Result{ExitCode: 0}, nil // -t ok
	}}
	b, fs := newBrokerWithFS(t, runner)
	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "webserver.apply",
		Input: mustJSON(t, map[string]any{
			"vhosts":   []map[string]string{{"name": "hps1", "config": "OK"}},
			"listener": "OK",
		}),
	}); err != nil {
		t.Fatalf("valid config should not error even if the server is down: %v", err)
	}
	if got, ok := fs.Written("/usr/local/lsws/conf/vhosts/hps1/vhconf.conf"); !ok || got != "OK" {
		t.Fatalf("config should remain written, got %q ok=%v", got, ok)
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
