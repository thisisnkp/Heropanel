package broker_test

import (
	"context"
	"strings"
	"testing"

	brokerd "github.com/thisisnkp/nexpanel/broker"
	"github.com/thisisnkp/nexpanel/broker/exec"
)

// engine=litespeed_enterprise writes the LSWS httpd-syntax config and reloads via
// lswsctrl.
func TestWebserverApplyLiteSpeedEnterprise(t *testing.T) {
	var ran []string
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		ran = append(ran, c.Path+" "+strings.Join(c.Args, " "))
		return exec.Result{ExitCode: 0}, nil
	}}
	b, fs := newBrokerWithFS(t, runner)

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "webserver.apply",
		Input: mustJSON(t, map[string]any{
			"engine": "litespeed_enterprise", "vhosts": []any{}, "listener": "<VirtualHost *:80>\n</VirtualHost>\n",
		}),
	})
	if err != nil {
		t.Fatalf("apply lsws: %v", err)
	}
	if _, ok := fs.Written("/usr/local/lsws/conf/httpd_config.conf"); !ok {
		t.Fatal("LiteSpeed config not written")
	}
	var reloaded bool
	for _, r := range ran {
		if strings.Contains(r, "lswsctrl reload") {
			reloaded = true
		}
	}
	if !reloaded {
		t.Fatalf("lswsctrl reload was not run: %v", ran)
	}
}

// A retired engine name falls back to OpenLiteSpeed instead of failing.
//
// This is not defensive padding: nginx and apache were supported engines in
// earlier releases, so an upgraded install can still have one of them recorded
// in its setup row and send it here. Refusing would leave that panel unable to
// apply any config at all — including the config that would fix it.
func TestWebserverApplyRetiredEngineFallsBackToOLS(t *testing.T) {
	for _, engine := range []string{"nginx", "apache", ""} {
		runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
			return exec.Result{ExitCode: 0}, nil
		}}
		b, fs := newBrokerWithFS(t, runner)

		_, err := b.Invoke(context.Background(), brokerd.Request{
			Capability: "webserver.apply",
			Input: mustJSON(t, map[string]any{
				"engine": engine, "vhosts": []any{}, "listener": "listener NexPanelHTTP {}",
			}),
		})
		if err != nil {
			t.Fatalf("apply %q: %v", engine, err)
		}
		if _, ok := fs.Written("/usr/local/lsws/conf/nexpanel.conf"); !ok {
			t.Fatalf("engine %q should have written the OpenLiteSpeed config", engine)
		}
		if _, ok := fs.Written("/etc/nginx/conf.d/nexpanel.conf"); ok {
			t.Fatalf("engine %q wrote an nginx config; that engine is gone", engine)
		}
	}
}
