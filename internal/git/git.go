// Package git implements Git-based deployments: a site pulls its content/app
// from a Git repository, builds it as the site's own unprivileged Linux user,
// and activates the result with an atomic, reversible release swap. It reuses the
// Sites module's per-site isolation (no new privilege) and drives the privileged
// broker capabilities git.deploy / git.rollback. See docs/11-git-deployments.md.
package git

import (
	"context"
	"database/sql"
	"net/url"
	"regexp"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// keepReleases is how many past releases to retain per site (the live one is
// never pruned). Passed through to the broker's git.deploy.
const keepReleases = 5

// Trigger records what initiated a deployment.
const (
	TriggerManual   = "manual"
	TriggerWebhook  = "webhook"
	TriggerRollback = "rollback"
)

// Deployment status values.
const (
	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusSuccess    = "success"
	StatusFailed     = "failed"
	StatusRolledBack = "rolled_back"
)

// Source is the API view of a site's Git source.
type Source struct {
	UID          string `json:"uid"`
	RepoURL      string `json:"repo_url"`
	Branch       string `json:"branch"`
	BuildCommand string `json:"build_command"`
	WebRoot      string `json:"web_root"`
	// WebhookURL is the push endpoint; the secret is embedded as a query param so
	// it is shown once and never returned in list responses.
	WebhookURL string `json:"webhook_url,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// Deployment is the API view of one deploy in a site's history.
type Deployment struct {
	UID        string `json:"uid"`
	CommitSHA  string `json:"commit_sha"`
	Status     string `json:"status"`
	Trigger    string `json:"trigger"`
	Log        string `json:"log,omitempty"`
	CreatedAt  string `json:"created_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// SourceRecord is the persistence row for a site's Git source.
type SourceRecord struct {
	ID            int64  `db:"id"`
	UID           string `db:"uid"`
	SiteID        int64  `db:"site_id"`
	RepoURL       string `db:"repo_url"`
	Branch        string `db:"branch"`
	BuildCommand  string `db:"build_command"`
	WebRoot       string `db:"web_root"`
	WebhookSecret string `db:"webhook_secret"`
	CreatedAt     string `db:"created_at"`
	UpdatedAt     string `db:"updated_at"`
}

// DeploymentRecord is the persistence row for a deployment.
type DeploymentRecord struct {
	ID          int64          `db:"id"`
	UID         string         `db:"uid"`
	SiteID      int64          `db:"site_id"`
	SourceID    int64          `db:"source_id"`
	CommitSHA   string         `db:"commit_sha"`
	Status      string         `db:"status"`
	TriggerKind string         `db:"trigger_kind"`
	ReleaseDir  string         `db:"release_dir"`
	Log         string         `db:"log"`
	CreatedAt   string         `db:"created_at"`
	FinishedAt  sql.NullString `db:"finished_at"`
}

// Repo is the persistence contract (implemented by internal/repository).
type Repo interface {
	UpsertSource(ctx context.Context, r *SourceRecord) error
	GetSourceBySiteID(ctx context.Context, siteID int64) (*SourceRecord, error)
	InsertDeployment(ctx context.Context, r *DeploymentRecord) error
	UpdateDeployment(ctx context.Context, r *DeploymentRecord) error
	ListDeployments(ctx context.Context, siteID int64, limit int) ([]DeploymentRecord, error)
	GetDeploymentByUID(ctx context.Context, uid string) (*DeploymentRecord, error)
}

// SiteRef is the minimal site identity/paths a deploy needs.
type SiteRef struct {
	ID         int64
	UID        string
	LinuxUser  string
	HomeDir    string
	DeployMode string
}

// Sites resolves a site's identity/paths by UID (implemented by an adapter over
// the site repository — keeps the git service off the concrete store).
type Sites interface {
	Resolve(ctx context.Context, siteUID string) (*SiteRef, error)
}

// SetSourceInput is the request to configure/replace a site's Git source.
type SetSourceInput struct {
	RepoURL      string
	Branch       string
	BuildCommand string
	WebRoot      string
}

// ── validation ───────────────────────────────────────────────────────────────

var (
	// reRef: a git branch / relative subpath token. Deliberately strict so the
	// value is safe to build filesystem paths from and cannot masquerade as a
	// CLI flag. No whitespace, no shell metacharacters.
	reRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
)

func validateSetSource(in *SetSourceInput) error {
	in.RepoURL = strings.TrimSpace(in.RepoURL)
	in.Branch = strings.TrimSpace(in.Branch)
	in.BuildCommand = strings.TrimSpace(in.BuildCommand)
	in.WebRoot = strings.Trim(strings.TrimSpace(in.WebRoot), "/")

	if err := validateRepoURL(in.RepoURL); err != nil {
		return err
	}
	if in.Branch == "" {
		in.Branch = "main"
	}
	if !reRef.MatchString(in.Branch) || strings.Contains(in.Branch, "..") {
		return errx.Validation("invalid_branch", "Branch contains invalid characters.",
			errx.Field{Field: "branch", Code: "invalid", Message: "invalid branch"})
	}
	if in.WebRoot != "" && (!reRef.MatchString(in.WebRoot) || strings.Contains(in.WebRoot, "..")) {
		return errx.Validation("invalid_web_root", "Web root must be a relative path with no '..'.",
			errx.Field{Field: "web_root", Code: "invalid", Message: "invalid web root"})
	}
	if len(in.BuildCommand) > 1000 {
		return errx.Validation("build_command_too_long", "Build command is too long (max 1000 chars).")
	}
	if strings.ContainsRune(in.BuildCommand, '\x00') {
		return errx.Validation("invalid_build_command", "Build command contains an invalid byte.")
	}
	return nil
}

// validateRepoURL accepts only clean https:// URLs (slice 1: public repos). SSH
// and credentialed URLs arrive with the encrypted-secret store (docs/11 §1).
func validateRepoURL(raw string) error {
	invalid := errx.Validation("invalid_repo_url", "A valid https:// repository URL is required.",
		errx.Field{Field: "repo_url", Code: "invalid", Message: "invalid url"})
	if raw == "" || len(raw) > 512 {
		return invalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path == "" {
		return invalid
	}
	// No credentials in the URL, and no characters that could be misread as an
	// argv flag or whitespace-split by any downstream tool.
	if u.User != nil || strings.ContainsAny(raw, " \t\r\n") || strings.HasPrefix(raw, "-") {
		return invalid
	}
	return nil
}
