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
	CreatedAt     string     `json:"created_at"`
}

// CreateInput is the request to create a site.
type CreateInput struct {
	Name          string
	PrimaryDomain string
	Type          Type
	DeployMode    DeployMode
	OwnerID       int64
}
