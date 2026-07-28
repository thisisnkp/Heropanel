package webmail

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The rendered config points Roundcube at the LOCAL MTAs over TLS and carries
// no mailbox password (the %u/%p placeholders are Roundcube's own login
// substitution, resolved per session — the panel never sees a password).
func TestRenderConfigWiresLocalMTAs(t *testing.T) {
	cfg := RenderConfig(Config{
		IMAPHost: "tls://127.0.0.1", IMAPPort: 143,
		SMTPHost: "tls://127.0.0.1", SMTPPort: 587,
		DESKey: "SECRETKEY", DBPath: "/var/lib/heropanel/webmail/roundcube.db",
		TempDir: "/var/lib/heropanel/webmail/temp", LogDir: "/var/lib/heropanel/webmail/logs",
		SkipCertVerify: true, ProductName: "HeroPanel Webmail",
	})
	for _, want := range []string{
		"$config['imap_host'] = 'tls://127.0.0.1:143';",
		"$config['smtp_host'] = 'tls://127.0.0.1:587';",
		"sqlite:////var/lib/heropanel/webmail/roundcube.db",
		"$config['des_key'] = 'SECRETKEY';",
		"'verify_peer' => false",
		"enable_installer'] = false",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q", want)
		}
	}
	// The panel must never bake a real password into the config.
	if strings.Contains(cfg, "smtp_pass") && !strings.Contains(cfg, "'%p'") {
		t.Error("smtp_pass is not the per-session placeholder")
	}
}

// fakeGW records invocations and can fail a named capability.
type fakeGW struct {
	calls []string
	fail  string
}

func (g *fakeGW) Invoke(_ context.Context, cap string, _ any) (map[string]any, error) {
	g.calls = append(g.calls, cap)
	if cap == g.fail {
		return nil, errors.New(cap + " failed")
	}
	if cap == "webmail.status" {
		return map[string]any{"installed": true}, nil
	}
	return map[string]any{}, nil
}
func (g *fakeGW) Health(context.Context) error { return nil }

type fakeReloader struct{ reloaded bool }

func (r *fakeReloader) ReapplyWebserver(context.Context) error { r.reloaded = true; return nil }

// Install lays down the runtime (webmail.install), the FPM pool (php.write_pool)
// and re-applies the web server so the vhost serves.
func TestInstallProvisionsPoolAndReloads(t *testing.T) {
	gw := &fakeGW{}
	rl := &fakeReloader{}
	s := NewService(gw, "webmail.shop.test", "8.3").WithReloader(rl)

	st, err := s.Install(t.Context())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !st.Installed || st.URL != "https://webmail.shop.test/" {
		t.Errorf("status = %+v", st)
	}
	joined := strings.Join(gw.calls, ",")
	if !strings.Contains(joined, "webmail.install") || !strings.Contains(joined, "php.write_pool") {
		t.Errorf("calls = %v", gw.calls)
	}
	if !rl.reloaded {
		t.Error("the web server was not re-applied")
	}
}

// With no hostname configured, webmail is disabled and contributes no vhost.
func TestDisabledWhenNoHostname(t *testing.T) {
	s := NewService(&fakeGW{}, "", "")
	if s.Enabled() || s.Available() {
		t.Error("webmail reports enabled with no hostname")
	}
	if v := s.SystemVhosts(context.Background()); v != nil {
		t.Errorf("a disabled webmail contributed a vhost: %+v", v)
	}
	if _, err := s.Install(t.Context()); err == nil {
		t.Fatal("install succeeded with no hostname")
	}
}

// The system vhost is a PHP vhost on the webmail host, rooted at the app dir.
func TestSystemVhostShape(t *testing.T) {
	s := NewService(&fakeGW{}, "webmail.shop.test", "8.3")
	v := s.SystemVhosts(context.Background())
	if len(v) != 1 {
		t.Fatalf("want 1 vhost, got %d", len(v))
	}
	if !v[0].IsPHP || v[0].DocumentRoot != AppDir || v[0].PrimaryDomain != "webmail.shop.test" {
		t.Errorf("vhost = %+v", v[0])
	}
}
