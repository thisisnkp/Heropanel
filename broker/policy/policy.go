// Package policy holds the broker's capability policy: which privileged
// capabilities are enabled and the constraints that bound them (allowed service
// names, allowed filesystem roots, minimum uid). The policy is loaded from
// /etc/heropanel/broker/policy.yaml (root-owned, 0600) at startup; this package
// defines the model and its checks. See docs/05-security-architecture.md.
package policy

import (
	"path"
	"strings"
)

// Policy is the broker's effective authorization policy.
type Policy struct {
	// Enabled maps capability name -> whether it may run at all. A capability
	// absent from this map is treated as disabled (deny by default).
	Enabled map[string]bool

	// Services is the allowlist of service names ServiceRestart may act on.
	Services []string

	// PathRoots is the allowlist of absolute roots under which file operations
	// (and site home directories) must reside.
	PathRoots []string

	// UIDMin is the minimum uid permitted for created system users, keeping
	// panel-created site users out of the system/reserved range.
	UIDMin int
}

// Default returns a conservative starting policy for a single-node install.
func Default() Policy {
	return Policy{
		Enabled: map[string]bool{
			"service.restart":    true,
			"system_user.create": true,
			"system_user.delete": true,
			"site.create_dirs":   true,
			"site.remove_dirs":   true,
			"webserver.apply":    true,
			"php.write_pool":     true,
		},
		Services: []string{
			"lshttpd", "openlitespeed", "mariadb", "redis",
			"postfix", "dovecot", "nftables",
		},
		PathRoots: []string{"/srv/heropanel/sites"},
		UIDMin:    20000,
	}
}

// CapabilityEnabled reports whether the named capability may run.
func (p Policy) CapabilityEnabled(name string) bool {
	return p.Enabled[name]
}

// ServiceAllowed reports whether name is on the service allowlist. It also
// accepts templated instances of an allowlisted unit (e.g. "php-fpm@site1" when
// "php-fpm" is allowed).
func (p Policy) ServiceAllowed(name string) bool {
	for _, s := range p.Services {
		if name == s {
			return true
		}
		if i := strings.IndexByte(name, '@'); i >= 0 && name[:i] == s {
			return true
		}
	}
	return false
}

// PathAllowed reports whether pth is confined to one of the allowlisted roots.
// The path is cleaned first, so traversal sequences (".." escaping a root) are
// rejected. Unix path semantics are used regardless of the build OS because the
// broker always operates on the Linux target's filesystem.
func (p Policy) PathAllowed(pth string) bool {
	cp := path.Clean(pth)
	if !path.IsAbs(cp) {
		return false
	}
	for _, root := range p.PathRoots {
		r := path.Clean(root)
		if cp == r || strings.HasPrefix(cp, r+"/") {
			return true
		}
	}
	return false
}
