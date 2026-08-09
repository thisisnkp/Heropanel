package installer

import (
	"context"
	"fmt"
)

// Package management is the one place the execute path genuinely differs across
// distributions. Everything else (users, dirs, systemd units, the panel config)
// is identical; only "install these packages" and the concrete package names
// diverge between apt and dnf. Isolating that here keeps the executor
// distro-agnostic.

// pkgNames maps a logical package to its per-manager concrete name. A logical
// name absent from a manager's row means "same as the logical name".
var pkgNames = map[string]map[string]string{
	"redis": {"apt": "redis-server", "dnf": "redis"},
	// mariadb-server happens to be identical on both, listed for clarity.
	"mariadb": {"apt": "mariadb-server", "dnf": "mariadb-server"},
}

// resolvePkg returns the concrete package name for a logical name under mgr.
func resolvePkg(mgr, logical string) string {
	if row, ok := pkgNames[logical]; ok {
		if name, ok := row[mgr]; ok {
			return name
		}
	}
	return logical
}

// pkgRefresh updates the package index. It is a no-op for dnf (which refreshes
// metadata on demand) and `apt-get update` for apt. Called once before the
// first install so repeated installs don't each re-hit the network.
func pkgRefresh(ctx context.Context, r Runner, mgr string) error {
	switch mgr {
	case "apt":
		return r.Run(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", "update", "-y")
	case "dnf":
		return nil
	default:
		return fmt.Errorf("unsupported package manager %q", mgr)
	}
}

// pkgInstall installs the given logical packages, resolving names per manager.
// It is idempotent: apt/dnf treat an already-installed package as success.
func pkgInstall(ctx context.Context, r Runner, mgr string, logical ...string) error {
	names := make([]string, len(logical))
	for i, l := range logical {
		names[i] = resolvePkg(mgr, l)
	}
	switch mgr {
	case "apt":
		args := append([]string{"install", "-y", "--no-install-recommends"}, names...)
		return r.Run(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", args...)
	case "dnf":
		args := append([]string{"install", "-y"}, names...)
		return r.Run(ctx, nil, "dnf", args...)
	default:
		return fmt.Errorf("unsupported package manager %q", mgr)
	}
}
