package policy_test

import (
	"testing"

	"github.com/thisisnkp/heropanel/broker/policy"
)

func TestCapabilityEnabledDenyByDefault(t *testing.T) {
	p := policy.Default()
	if !p.CapabilityEnabled("service.restart") {
		t.Fatal("service.restart should be enabled by default")
	}
	if p.CapabilityEnabled("unknown.capability") {
		t.Fatal("unknown capabilities must be denied by default")
	}
}

func TestServiceAllowed(t *testing.T) {
	p := policy.Default()
	if !p.ServiceAllowed("mariadb") {
		t.Fatal("mariadb should be allowed")
	}
	if p.ServiceAllowed("sshd") {
		t.Fatal("sshd is not on the allowlist and must be denied")
	}

	// Templated instances of an allowlisted unit are permitted.
	p.Services = append(p.Services, "php-fpm")
	if !p.ServiceAllowed("php-fpm@site1") {
		t.Fatal("templated instance php-fpm@site1 should be allowed")
	}
	if p.ServiceAllowed("php-fpm-evil") {
		t.Fatal("php-fpm-evil must not match php-fpm")
	}
}

func TestPathAllowedConfinement(t *testing.T) {
	p := policy.Policy{PathRoots: []string{"/srv/heropanel/sites"}}

	allowed := []string{
		"/srv/heropanel/sites",
		"/srv/heropanel/sites/1",
		"/srv/heropanel/sites/1/public/index.php",
	}
	for _, pth := range allowed {
		if !p.PathAllowed(pth) {
			t.Errorf("expected %q to be allowed", pth)
		}
	}

	denied := []string{
		"/etc/passwd",
		"/srv/heropanel/sitesX",                 // prefix but not a path boundary
		"/srv/heropanel/sites/../../etc/passwd", // traversal escaping the root
		"srv/heropanel/sites/1",                 // not absolute
		"/srv/heropanel/sites/1/../../../etc/shadow",
	}
	for _, pth := range denied {
		if p.PathAllowed(pth) {
			t.Errorf("expected %q to be denied", pth)
		}
	}
}
