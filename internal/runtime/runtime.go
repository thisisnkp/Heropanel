// Package runtime manages app runtimes: a supervised long-running process (a
// per-site systemd unit run as the site's unprivileged user in its current
// release) that OpenLiteSpeed reverse-proxies to. It completes the Git-deploy
// story for Node/Python/Go apps. Privileged work (systemd unit, service control)
// goes through the broker; the reverse-proxy vhost is rendered by internal/
// webserver. See docs/12-app-runtimes.md.
package runtime

import (
	"context"
	"regexp"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Runtime labels (informational; the operator supplies the actual command).
const (
	RuntimeNode    = "node"
	RuntimePython  = "python"
	RuntimeGo      = "go"
	RuntimeGeneric = "generic"
)

// Status values.
const (
	StatusStopped = "stopped"
	StatusRunning = "running"
	StatusError   = "error"
)

// supportedRuntimes is the label allowlist.
var supportedRuntimes = map[string]bool{
	RuntimeNode: true, RuntimePython: true, RuntimeGo: true, RuntimeGeneric: true,
}

// Runtime is the API view of a site's app runtime.
type Runtime struct {
	UID       string            `json:"uid"`
	Runtime   string            `json:"runtime"`
	Command   string            `json:"command"`
	Port      int               `json:"port"`
	Env       map[string]string `json:"env"`
	Status    string            `json:"status"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

// Record is the persistence row. Env is stored as a JSON object string.
type Record struct {
	ID        int64  `db:"id"`
	UID       string `db:"uid"`
	SiteID    int64  `db:"site_id"`
	Runtime   string `db:"runtime"`
	Command   string `db:"command"`
	Port      int    `db:"port"`
	Env       string `db:"env"`
	Status    string `db:"status"`
	CreatedAt string `db:"created_at"`
	UpdatedAt string `db:"updated_at"`
}

// Repo is the persistence contract (implemented by internal/repository).
type Repo interface {
	Upsert(ctx context.Context, r *Record) error
	GetBySiteID(ctx context.Context, siteID int64) (*Record, error)
	SetStatus(ctx context.Context, siteID int64, status string) error
}

// SiteRef is the minimal site identity a runtime needs.
type SiteRef struct {
	ID        int64
	UID       string
	LinuxUser string // also the vhost id
	HomeDir   string
}

// Sites resolves a site by UID (adapter over the site repository).
type Sites interface {
	Resolve(ctx context.Context, siteUID string) (*SiteRef, error)
}

// SetInput is the request to configure a site's runtime.
type SetInput struct {
	Runtime string
	Command string
	Port    int
	Env     map[string]string
}

// ── validation ───────────────────────────────────────────────────────────────

// reEnvKey is a conventional environment variable name.
var reEnvKey = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func validateSet(in *SetInput) error {
	in.Command = strings.TrimSpace(in.Command)
	if in.Runtime == "" {
		in.Runtime = RuntimeGeneric
	}
	if !supportedRuntimes[in.Runtime] {
		return errx.Validation("invalid_runtime", "Unsupported runtime.",
			errx.Field{Field: "runtime", Code: "unsupported", Message: "node|python|go|generic"})
	}
	if in.Command == "" || len(in.Command) > 1000 || strings.ContainsRune(in.Command, '\x00') {
		return errx.Validation("invalid_command", "A start command is required (max 1000 chars).",
			errx.Field{Field: "command", Code: "invalid", Message: "invalid command"})
	}
	if in.Port < 1024 || in.Port > 65535 {
		return errx.Validation("invalid_port", "Port must be between 1024 and 65535.",
			errx.Field{Field: "port", Code: "invalid", Message: "out of range"})
	}
	for k, v := range in.Env {
		if !reEnvKey.MatchString(k) {
			return errx.Validation("invalid_env_key", "Invalid environment variable name: "+k)
		}
		if strings.ContainsAny(v, "\x00\n\r") || len(v) > 2000 {
			return errx.Validation("invalid_env_value", "Invalid value for "+k+".")
		}
	}
	return nil
}
