// Package site implements the Sites feature: creating and managing isolated
// hosted sites. Each site gets a dedicated Linux user, group, and directory tree
// (provisioned via the privileged broker), realizing the isolation model in
// docs/05. This is the first feature to drive broker.Gateway.
package site

// Status is a site's lifecycle state.
type Status string

const (
	StatusProvisioning Status = "provisioning"
	StatusActive       Status = "active"
	StatusError        Status = "error"
	StatusSuspended    Status = "suspended"
	StatusDisabled     Status = "disabled"
)

// Type is the application kind hosted by the site.
type Type string

const (
	TypeStatic Type = "static"
	TypePHP    Type = "php"
	// TypeProxy is an app site: OpenLiteSpeed reverse-proxies to a supervised
	// process (managed by internal/runtime) instead of serving files.
	TypeProxy Type = "proxy"
)

// Domain kinds, mirrored from internal/domain. They are duplicated here (rather
// than imported) so the site service depends only on its own Domains interface —
// internal/domain adapts to it, not the other way round.
const (
	DomainKindPrimary  = "primary"
	DomainKindAlias    = "alias"
	DomainKindRedirect = "redirect"
)

// DeployMode determines how content reaches the site (and gates features like
// the File Manager, which is bare-metal only).
type DeployMode string

const (
	DeployBaremetal DeployMode = "baremetal"
	DeployGit       DeployMode = "git"
	DeployDocker    DeployMode = "docker"
)

// Site is the API/domain view of a hosted site.
type Site struct {
	UID           string     `json:"uid"`
	Name          string     `json:"name"`
	PrimaryDomain string     `json:"primary_domain"`
	Type          Type       `json:"type"`
	DeployMode    DeployMode `json:"deploy_mode"`
	Status        Status     `json:"status"`
	Webserver     string     `json:"webserver"`
	DocumentRoot  string     `json:"document_root"`
	SystemUser    string     `json:"system_user"`
	// AppProject is the one-click Docker app this proxy site fronts, if any. Empty
	// for ordinary sites.
	AppProject string `json:"app_project,omitempty"`
	// WAFEnabled reports whether the ModSecurity + OWASP CRS firewall is on.
	WAFEnabled bool   `json:"waf_enabled"`
	CreatedAt  string `json:"created_at"`
	// Stack is what the site actually runs, as the panel presents it: "static",
	// "php", "node", "python" or "wp".
	//
	// It is not the same question as Type, and the difference is why this field
	// exists. Type is how the vhost is built — files, PHP, or a reverse proxy —
	// and three different stacks share the proxy answer. A client that only has
	// Type has to guess whether a proxy site is Node or Python, and it will
	// guess wrong for half of them. The server knows: it holds the runtime
	// record. So it answers, and no client has to.
	Stack string `json:"stack"`

	// DNSStatus reports whether the primary domain had proven ownership
	// ("verified" — a free/parked domain or a subdomain of one) at creation
	// time, or none ("unverified" — the operator should add DNS and verify it
	// at /domains). Empty when no DomainRegistry is wired. This is a point-in-
	// time snapshot from creation, not re-checked on later reads — the parked-
	// domain list is the live source of truth for verification status.
	DNSStatus string `json:"dns_status,omitempty"`
}

// CreateInput is the request to create a site.
type CreateInput struct {
	Name          string
	PrimaryDomain string
	Type          Type
	DeployMode    DeployMode
	OwnerID       int64
	// AppProject, when set, makes this a proxy site backed by a one-click Docker
	// app: the vhost reverse-proxies to the app's published loopback port,
	// resolved live at render time. It forces Type to proxy.
	AppProject string
}
