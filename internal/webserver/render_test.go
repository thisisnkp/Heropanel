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
