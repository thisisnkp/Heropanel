package broker_test

import (
	"context"
	"strings"
	"testing"

	brokerd "github.com/thisisnkp/nexpanel/broker"
	"github.com/thisisnkp/nexpanel/broker/exec"
)

// webserver.apply with engine=nginx writes the nginx config, tests it with
// `nginx -t`, and reloads nginx.
func TestWebserverApplyNginx(t *testing.T) {
	var ran []string
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		ran = append(ran, c.Path+" "+strings.Join(c.Args, " "))
		return exec.Result{ExitCode: 0}, nil
	}}
	b, fs := newBrokerWithFS(t, runner)

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "webserver.apply",
		Input: mustJSON(t, map[string]any{
			"engine":   "nginx",
			"vhosts":   []any{},
			"listener": "server {\n  listen 80;\n}\n",
		}),
	})
	if err != nil {
		t.Fatalf("apply nginx: %v", err)
	}
	if got, ok := fs.Written("/etc/nginx/conf.d/nexpanel.conf"); !ok || !strings.Contains(got, "listen 80") {
		t.Fatalf("nginx config not written: %q", got)
	}
	var tested, reloaded bool
	for _, r := range ran {
		if strings.Contains(r, "/usr/sbin/nginx -t") {
			tested = true
		}
		if strings.Contains(r, "systemctl reload nginx") {
			reloaded = true
		}
	}
	if !tested {
		t.Errorf("nginx -t was not run: %v", ran)
	}
	if !reloaded {
		t.Errorf("nginx was not reloaded: %v", ran)
	}
}

// A config nginx rejects (`nginx -t` non-zero) is rolled back.
func TestWebserverApplyNginxRollback(t *testing.T) {
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if strings.Contains(strings.Join(c.Args, " "), "-t") {
			return exec.Result{ExitCode: 1}, nil // invalid config
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	b, fs := newBrokerWithFS(t, runner)
	_ = fs.WriteFile("/etc/nginx/conf.d/nexpanel.conf", []byte("PRIOR"), 0o644)

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "webserver.apply",
		Input:      mustJSON(t, map[string]any{"engine": "nginx", "vhosts": []any{}, "listener": "server { bad"}),
	})
	if err == nil {
		t.Fatal("an invalid nginx config reported success")
	}
	if got, _ := fs.Written("/etc/nginx/conf.d/nexpanel.conf"); got != "PRIOR" {
		t.Fatalf("nginx config not rolled back: %q", got)
	}
}

// webserver.apply with engine=apache on Debian writes to conf-enabled, tests with
// apache2ctl, and reloads apache2.
func TestWebserverApplyApacheDebian(t *testing.T) {
	var ran []string
	runner := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		ran = append(ran, c.Path+" "+strings.Join(c.Args, " "))
		return exec.Result{ExitCode: 0}, nil
	}}
	b, fs := newBrokerWithFS(t, runner)
	_ = fs.WriteFile("/usr/bin/apt-config", []byte("x"), 0o755) // ⇒ Debian family

	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "webserver.apply",
		Input: mustJSON(t, map[string]any{
			"engine": "apache", "vhosts": []any{}, "listener": "<VirtualHost *:80>\n</VirtualHost>\n",
		}),
	})
	if err != nil {
		t.Fatalf("apply apache: %v", err)
	}
	if _, ok := fs.Written("/etc/apache2/conf-enabled/nexpanel.conf"); !ok {
		t.Fatal("apache config not written to the Debian path")
	}
	var tested, reloaded bool
	for _, r := range ran {
		if strings.Contains(r, "/usr/sbin/apache2ctl -t") {
			tested = true
		}
		if strings.Contains(r, "systemctl reload apache2") {
			reloaded = true
		}
	}
	if !tested || !reloaded {
		t.Fatalf("apache not tested/reloaded: %v", ran)
	}
}

// engine=litespeed_enterprise writes the LSWS apache-style config and reloads via
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
