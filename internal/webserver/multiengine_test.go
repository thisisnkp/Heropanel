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

// ── Nginx ────────────────────────────────────────────────────────────────────

func TestRenderNginxPHP(t *testing.T) {
	cfg, err := webserver.RenderFor(webserver.EngineNginx, []webserver.Site{phpSite()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"server {",
		"listen 80;",
		"server_name acme.example.com www.acme.example.com;",
		"root /srv/nexpanel/sites/1/public;",
		"fastcgi_pass unix:/run/nexpanel/fpm/nps1.sock;",
		"location ~ \\.php$ {",
		"access_log /srv/nexpanel/sites/1/logs/access.log;",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("nginx config missing %q:\n%s", want, cfg)
		}
	}
}

func TestRenderNginxProxyAndForceHTTPS(t *testing.T) {
	s := phpSite()
	s.IsPHP = false
	s.ProxyTarget = "127.0.0.1:3000"
	s.ForceHTTPS = true
	cfg, err := webserver.RenderFor(webserver.EngineNginx, []webserver.Site{s})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(cfg, "proxy_pass http://127.0.0.1:3000;") {
		t.Fatalf("expected proxy_pass:\n%s", cfg)
	}
	if !strings.Contains(cfg, `if ($scheme != "https")`) {
		t.Fatalf("expected force-https guard:\n%s", cfg)
	}
	// A proxied site does not also serve PHP.
	if strings.Contains(cfg, "fastcgi_pass") {
		t.Fatalf("proxy site should not have fastcgi:\n%s", cfg)
	}
}

func TestRenderNginxSuspended503(t *testing.T) {
	s := phpSite()
	s.Suspended = true
	cfg, _ := webserver.RenderFor(webserver.EngineNginx, []webserver.Site{s})
	if !strings.Contains(cfg, "return 503;") {
		t.Fatalf("suspended nginx site must return 503:\n%s", cfg)
	}
	// Still keeps its server_name so the domain stays mapped here, not to another site.
	if !strings.Contains(cfg, "server_name acme.example.com") {
		t.Fatalf("suspended site must keep its server_name:\n%s", cfg)
	}
	if strings.Contains(cfg, "fastcgi_pass") {
		t.Fatalf("suspended site must not run PHP:\n%s", cfg)
	}
}

// ── Apache (and LiteSpeed Enterprise, which shares it) ───────────────────────

func TestRenderApachePHP(t *testing.T) {
	cfg, err := webserver.RenderFor(webserver.EngineApache, []webserver.Site{phpSite()})
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
			t.Fatalf("apache config missing %q:\n%s", want, cfg)
		}
	}
	// The primary must not also appear as an alias.
	if strings.Contains(cfg, "ServerAlias acme.example.com\n") {
		t.Fatalf("primary domain must not be a ServerAlias:\n%s", cfg)
	}
}

func TestRenderApacheRedirectAndSuspended(t *testing.T) {
	s := phpSite()
	s.Redirects = []webserver.Redirect{{From: "old.example.com", To: "https://new.example.com", Code: 301}}
	cfg, _ := webserver.RenderFor(webserver.EngineApache, []webserver.Site{s})
	if !strings.Contains(cfg, "RewriteEngine On") || !strings.Contains(cfg, `RewriteCond %{HTTP_HOST} ^old\.example\.com$ [NC]`) {
		t.Fatalf("expected apache redirect rules:\n%s", cfg)
	}

	s2 := phpSite()
	s2.Suspended = true
	cfg2, _ := webserver.RenderFor(webserver.EngineApache, []webserver.Site{s2})
	if !strings.Contains(cfg2, "RedirectMatch 503") {
		t.Fatalf("suspended apache site must 503:\n%s", cfg2)
	}
}

// LiteSpeed Enterprise reuses the Apache renderer.
func TestRenderLiteSpeedUsesApache(t *testing.T) {
	cfg, err := webserver.RenderFor(webserver.EngineLiteSpeed, []webserver.Site{phpSite()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(cfg, "<VirtualHost *:80>") {
		t.Fatalf("LiteSpeed Enterprise should render Apache config:\n%s", cfg)
	}
}

// An unknown engine falls back to OpenLiteSpeed.
func TestRenderForDefaultsToOLS(t *testing.T) {
	cfg, err := webserver.RenderFor("", []webserver.Site{phpSite()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(cfg, "listener NexPanelHTTP {") {
		t.Fatalf("empty engine should render OLS:\n%s", cfg)
	}
}
