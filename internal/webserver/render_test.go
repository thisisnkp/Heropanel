package webserver_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/webserver"
)

func TestRenderVhostStatic(t *testing.T) {
	cfg, err := webserver.RenderVhost(webserver.Site{
		VhostName:     "hps1",
		PrimaryDomain: "acme.example.com",
		DocumentRoot:  "/srv/heropanel/sites/1/public",
		LogDir:        "/srv/heropanel/sites/1/logs",
		IsPHP:         false,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(cfg, "docRoot                   /srv/heropanel/sites/1/public") {
		t.Fatalf("missing docRoot:\n%s", cfg)
	}
	if strings.Contains(cfg, "lsapi") || strings.Contains(cfg, "index.php") {
		t.Fatalf("static vhost should have no PHP handler:\n%s", cfg)
	}
}

func TestRenderVhostPHP(t *testing.T) {
	cfg, err := webserver.RenderVhost(webserver.Site{
		VhostName:     "hps2",
		PrimaryDomain: "php.example.com",
		DocumentRoot:  "/srv/heropanel/sites/2/public",
		LogDir:        "/srv/heropanel/sites/2/logs",
		IsPHP:         true,
		FpmSocket:     "/run/heropanel/fpm/hps2.sock",
		PhpBin:        "/usr/local/lsws/lsphp82/bin/lsphp",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"index.php", "lsapi:hps2_lsphp", "uds:///run/heropanel/fpm/hps2.sock", "/usr/local/lsws/lsphp82/bin/lsphp"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("PHP vhost missing %q:\n%s", want, cfg)
		}
	}
}

func TestRenderListener(t *testing.T) {
	out, err := webserver.RenderListener([]webserver.Site{
		{VhostName: "hps1", Home: "/srv/heropanel/sites/1", Domains: []string{"acme.example.com", "www.acme.example.com"}},
		{VhostName: "hps2", Home: "/srv/heropanel/sites/2", Domains: []string{"beta.example.com"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"virtualhost hps1 {",
		"virtualhost hps2 {",
		"listener HeroPanelHTTP {",
		"map                     hps1 acme.example.com, www.acme.example.com",
		"map                     hps2 beta.example.com",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("listener missing %q:\n%s", want, out)
		}
	}
}
