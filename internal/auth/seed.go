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
	// Impersonation is a grant apart from user.write: editing a user is acting on
	// their record, while impersonating one is acting *as* them, with their
	// permissions, in a fully audited session. It is deliberately not implied by
	// user.write so "can manage users" and "can become a user" stay separable.
	{"user.impersonate", "user", "impersonate", "Start an audited session acting as another user"},
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
	// Webhooks turn the audit stream into outbound notifications. Reading and
	// managing them is its own grant: a webhook receives a feed of what happened
	// (tenant-filtered for non-admins), so subscribing is a bigger act than any
	// single read.
	{"webhook.read", "webhook", "read", "View outbound webhooks and their deliveries"},
	{"webhook.write", "webhook", "write", "Create, edit and delete outbound webhooks"},
	// Monitoring is host-wide, like Docker: viewing live node/site/container
	// metrics is a read; configuring alert thresholds and notification targets is
	// a write. Live dashboards are subscription-gated, so this read also gates the
	// realtime `monitor:*` channels.
	{"monitor.read", "monitor", "read", "View live and historical metrics"},
	{"monitor.write", "monitor", "write", "Configure metric alerts and notification targets"},
	// Mail is its own resource: a mail domain is not a site, and handing
	// someone site.write must not hand them everyone's mailboxes.
	{"mail.read", "mail", "read", "View mail domains, mailboxes and aliases"},
	{"mail.write", "mail", "write", "Manage mail domains, mailboxes and aliases"},
	// Security is host-wide and high-stakes: firewall, malware scans, quarantine.
	// A firewall change can lock the whole box out, so it carries its own pair.
	{"security.read", "security", "read", "View firewall rules, scans and quarantine"},
	{"security.write", "security", "write", "Change the firewall, run scans, manage quarantine"},
	// The module marketplace. Browsing what is offered is a read; installing,
	// enabling, or removing a module is host-wide and runs third-party code, so
	// managing carries its own grant apart from any single resource's write.
	{"module.read", "module", "read", "Browse the module marketplace and installed modules"},
	{"module.manage", "module", "manage", "Install, enable, disable and remove marketplace modules"},
	// First-run setup: choosing the webserver, database engine, and whether DNS
	// and mail are managed here, then provisioning the host to match. It shapes
	// the whole box and runs once at install time, so it is reserved to
	// administrators rather than implied by any single resource's write.
	{"setup.manage", "setup", "manage", "Complete the first-run setup wizard and provision the hosting stack"},
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
