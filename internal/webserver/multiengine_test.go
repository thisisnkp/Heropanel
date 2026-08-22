package webserver_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/webserver"
)

func phpSite() webserver.Site {
	return webserver.Site{
		VhostName: "nps1", PrimaryDomain: "acme.example.com",
		Domains:      []string{"acme.example.com", "www.acme.example.com"},
		DocumentRoot: "/srv/nexpanel/sites/1/public",
		Home:         "/srv/nexpanel/sites/1", LogDir: "/srv/nexpanel/sites/1/logs",
		IsPHP: true, FpmSocket: "/run/nexpanel/fpm/nps1.sock", PhpBin: "/usr/sbin/php-fpm8.3",
	}
}

// ── LiteSpeed Enterprise ─────────────────────────────────────────────────────

func TestRenderLiteSpeedEnterprisePHP(t *testing.T) {
	cfg, err := webserver.RenderFor(webserver.EngineLiteSpeed, []webserver.Site{phpSite()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"<VirtualHost *:80>",
		"ServerName acme.example.com",
		"ServerAlias www.acme.example.com",
		"DocumentRoot /srv/nexpanel/sites/1/public",
		`SetHandler "proxy:unix:/run/nexpanel/fpm/nps1.sock|fcgi://localhost"`,
		"AllowOverride All",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("LSWE config missing %q:\n%s", want, cfg)
		}
	}
	// The primary must not also appear as an alias.
	if strings.Contains(cfg, "ServerAlias acme.example.com\n") {
		t.Fatalf("primary domain must not be a ServerAlias:\n%s", cfg)
	}
}

func TestRenderLiteSpeedEnterpriseRedirectAndSuspended(t *testing.T) {
	s := phpSite()
	s.Redirects = []webserver.Redirect{{From: "old.example.com", To: "https://new.example.com", Code: 301}}
	cfg, _ := webserver.RenderFor(webserver.EngineLiteSpeed, []webserver.Site{s})
	if !strings.Contains(cfg, "RewriteEngine On") || !strings.Contains(cfg, `RewriteCond %{HTTP_HOST} ^old\.example\.com$ [NC]`) {
		t.Fatalf("expected redirect rules:\n%s", cfg)
	}

	s2 := phpSite()
	s2.Suspended = true
	cfg2, _ := webserver.RenderFor(webserver.EngineLiteSpeed, []webserver.Site{s2})
	if !strings.Contains(cfg2, "RedirectMatch 503") {
		t.Fatalf("suspended site must 503:\n%s", cfg2)
	}
}

// ── Everything else is OpenLiteSpeed ─────────────────────────────────────────

// An unknown engine falls back to OpenLiteSpeed. This covers more than a typo:
// nginx and apache were supported engines once, so a panel upgraded from an
// older release can still have "nginx" persisted in its setup row. Falling back
// means that install keeps serving on the engine it now actually runs, instead
// of rendering an empty config or refusing to apply.
func TestRenderForFallsBackToOLS(t *testing.T) {
	for _, engine := range []webserver.Engine{"", "nginx", "apache", "caddy"} {
		cfg, err := webserver.RenderFor(engine, []webserver.Site{phpSite()})
		if err != nil {
			t.Fatalf("render %q: %v", engine, err)
		}
		if !strings.Contains(cfg, "listener NexPanelHTTP {") {
			t.Fatalf("engine %q should render OLS:\n%s", engine, cfg)
		}
	}
}
