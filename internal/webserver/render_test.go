package webserver_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/webserver"
)

func staticSite() webserver.Site {
	return webserver.Site{
		VhostName: "hps1", PrimaryDomain: "acme.example.com",
		Domains: []string{"acme.example.com"}, DocumentRoot: "/srv/heropanel/sites/1/public",
		Home: "/srv/heropanel/sites/1", LogDir: "/srv/heropanel/sites/1/logs",
	}
}

func TestRenderConfigStatic(t *testing.T) {
	cfg, err := webserver.RenderConfig([]webserver.Site{staticSite()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"virtualhost hps1 {",
		"docRoot                 /srv/heropanel/sites/1/public",
		"listener HeroPanelHTTP {",
		"map                     hps1 acme.example.com",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("static config missing %q:\n%s", want, cfg)
		}
	}
	// A static site must have no PHP handler wiring.
	for _, no := range []string{"extProcessor", "scriptHandler", "fcgi", "index.php"} {
		if strings.Contains(cfg, no) {
			t.Fatalf("static config should not contain %q:\n%s", no, cfg)
		}
	}
}

func TestRenderConfigPHP(t *testing.T) {
	s := staticSite()
	s.VhostName = "hps2"
	s.PrimaryDomain = "php.example.com"
	s.Domains = []string{"php.example.com"}
	s.IsPHP = true
	s.FpmSocket = "/run/heropanel/fpm/hps2.sock"
	s.PhpBin = "/usr/sbin/php-fpm8.3"

	cfg, err := webserver.RenderConfig([]webserver.Site{s})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"extProcessor hps_hps2 {",      // server-level handler
		"type                    fcgi", // FastCGI to php-fpm
		"uds:///run/heropanel/fpm/hps2.sock",
		"path                    /usr/sbin/php-fpm8.3", // OLS requires a path
		"autoStart               0",                    // external pool (php-fpm)
		"index.html, index.htm, index.php",
		"add                   fcgi:hps_hps2 php", // per-vhost scriptHandler
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("php config missing %q:\n%s", want, cfg)
		}
	}
	// The extProcessor must be declared before the vhost that references it.
	if strings.Index(cfg, "extProcessor hps_hps2") > strings.Index(cfg, "virtualhost hps2") {
		t.Fatal("extProcessor must precede its virtualhost")
	}
}

func TestRenderConfigProxy(t *testing.T) {
	s := staticSite()
	s.VhostName = "hps3"
	s.PrimaryDomain = "app.example.com"
	s.Domains = []string{"app.example.com"}
	s.ProxyTarget = "127.0.0.1:3000"

	cfg, err := webserver.RenderConfig([]webserver.Site{s})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"extProcessor proxy_hps3 {", // server-level proxy external app
		"type                    proxy",
		"address                 127.0.0.1:3000",
		"context / {", // vhost proxy context
		"handler               proxy_hps3",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("proxy config missing %q:\n%s", want, cfg)
		}
	}
	// The proxy external app must be declared before the vhost that references it.
	if strings.Index(cfg, "extProcessor proxy_hps3") > strings.Index(cfg, "virtualhost hps3") {
		t.Fatal("proxy extProcessor must precede its virtualhost")
	}
	// A proxy site is not a PHP site.
	if strings.Contains(cfg, "scriptHandler") || strings.Contains(cfg, "fcgi") {
		t.Fatalf("proxy config should not have PHP wiring:\n%s", cfg)
	}
}

func TestRenderConfigDomainsAndRewrites(t *testing.T) {
	s := staticSite()
	s.Domains = []string{"acme.example.com", "www.acme.example.com", "old.example.com"}
	s.ForceHTTPS = true
	s.Redirects = []webserver.Redirect{{From: "old.example.com", To: "https://acme.example.com", Code: 301}}

	cfg, err := webserver.RenderConfig([]webserver.Site{s})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		// Every domain (aliases and redirect hosts alike) maps to the vhost.
		"map                     hps1 acme.example.com, www.acme.example.com, old.example.com",
		"rewrite  {",
		"rules                 <<<END_rules",
		// The redirect host is regex-escaped and answered before force-HTTPS.
		`RewriteCond %{HTTP_HOST} ^old\.example\.com$ [NC]`,
		"RewriteRule ^(.*)$ https://acme.example.com$1 [R=301,L]",
		"RewriteCond %{HTTPS} !on",
		"RewriteRule ^(.*)$ https://%{HTTP_HOST}$1 [R=301,L]",
		"END_rules",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("config missing %q:\n%s", want, cfg)
		}
	}
	// The redirect must be evaluated before the force-HTTPS catch-all.
	if strings.Index(cfg, "https://acme.example.com$1") > strings.Index(cfg, "%{HTTPS} !on") {
		t.Fatal("redirect rules must precede the force-HTTPS rule")
	}
}

func TestRenderConfigNoRewriteBlockWhenNotNeeded(t *testing.T) {
	cfg, err := webserver.RenderConfig([]webserver.Site{staticSite()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// A plain site (no force-HTTPS, no redirects) must not force HTTPS — doing so
	// before a certificate exists would take it offline.
	if strings.Contains(cfg, "rewrite") || strings.Contains(cfg, "RewriteRule") {
		t.Fatalf("unexpected rewrite block:\n%s", cfg)
	}
}

func TestRenderConfigMultiSite(t *testing.T) {
	a := staticSite()
	b := staticSite()
	b.VhostName = "hps2"
	b.PrimaryDomain = "beta.example.com"
	b.Domains = []string{"beta.example.com"}

	cfg, err := webserver.RenderConfig([]webserver.Site{a, b})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"virtualhost hps1 {", "virtualhost hps2 {",
		"map                     hps1 acme.example.com", "map                     hps2 beta.example.com"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("multi-site config missing %q:\n%s", want, cfg)
		}
	}
}
