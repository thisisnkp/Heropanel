package auth

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/repository"
)

// PermWildcard is the superuser permission slug.
const PermWildcard = "*"

// seedPermission is a permission to ensure exists.
type seedPermission struct {
	slug, resource, action, desc string
}

// basePermissions is the Phase-0 permission catalog. It grows as modules add
// their own permissions.
var basePermissions = []seedPermission{
	{PermWildcard, "*", "*", "Full administrative access"},
	{"user.read", "user", "read", "View users"},
	{"user.write", "user", "write", "Create and modify users"},
	{"site.read", "site", "read", "View sites"},
	{"site.write", "site", "write", "Create and modify sites"},
	{"dns.read", "dns", "read", "View DNS zones and records"},
	{"dns.write", "dns", "write", "Modify DNS zones and records"},
	{"ssl.read", "ssl", "read", "View certificates"},
	{"ssl.write", "ssl", "write", "Issue and manage certificates"},
	{"database.read", "database", "read", "View databases"},
	{"database.write", "database", "write", "Create and manage databases"},
	{"git.read", "git", "read", "View Git sources and deployments"},
	{"git.write", "git", "write", "Configure Git sources and trigger deployments"},
	{"file.read", "file", "read", "Browse and download a site's files"},
	{"file.write", "file", "write", "Create, edit, upload, and delete a site's files"},
	{"terminal.use", "terminal", "use", "Open an interactive shell as a site's Linux user"},
	// Reading a recording is a bigger grant than opening your own shell: it is
	// reading a transcript of what someone else typed. Deleting one is bigger
	// still — destroying an audit artifact is precisely what an operator under
	// scrutiny would want — so it is grantable separately.
	{"terminal.recordings.read", "terminal", "recordings.read", "View and replay recorded terminal sessions"},
	{"terminal.recordings.delete", "terminal", "recordings.delete", "Delete recorded terminal sessions"},
	// Docker is host-wide rather than site-scoped, and stopping the container
	// serving a site is a different act from editing that site — so it carries
	// its own read/write pair instead of riding on site.*.
	{"docker.read", "docker", "read", "View containers, images, logs and stats"},
	{"docker.write", "docker", "write", "Start, stop, restart and remove containers; pull images"},
	{"system.read", "system", "read", "View system status"},
	{"system.write", "system", "write", "Change system configuration"},
	{"audit.read", "audit", "read", "View the audit log"},
	// Monitoring is host-wide, like Docker: viewing live node/site/container
	// metrics is a read; configuring alert thresholds and notification targets is
	// a write. Live dashboards are subscription-gated, so this read also gates the
	// realtime `monitor:*` channels.
	{"monitor.read", "monitor", "read", "View live and historical metrics"},
	{"monitor.write", "monitor", "write", "Configure metric alerts and notification targets"},
}

// seedRole is a role to ensure exists.
type seedRole struct {
	slug, name, desc string
}

var baseRoles = []seedRole{
	{"admin", "Administrator", "Full access to everything"},
	{"reseller", "Reseller", "Manages an isolated tenant of clients"},
	{"developer", "Developer", "Manages assigned sites and deployments"},
	{"client", "Client", "Manages their own sites"},
}

// SeedRBAC ensures the baseline permissions and system roles exist and that the
// admin role holds the superuser permission. It is idempotent.
func SeedRBAC(ctx context.Context, rbac *repository.RBACRepository) error {
	for _, p := range basePermissions {
		if err := rbac.EnsurePermission(ctx, p.slug, p.resource, p.action, p.desc); err != nil {
			return err
		}
	}
	for _, r := range baseRoles {
		if _, err := rbac.EnsureRole(ctx, r.slug, r.name, true, r.desc); err != nil {
			return err
		}
	}
	// The admin role is the superuser.
	return rbac.GrantPermission(ctx, "admin", PermWildcard)
}
