// Package php manages per-site PHP-FPM pools and the PHP version selector. Each
// PHP site gets a dedicated FPM pool listening on a private Unix socket, running
// as the site's Linux user with open_basedir confinement (docs/05 §3). hpd
// renders the pool config; the broker writes it and reloads php-fpm.
package php

import (
	"bytes"
	"context"
	"text/template"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// DefaultVersion is used when a PHP site does not specify a version.
const DefaultVersion = "8.2"

// SupportedVersions is the set of selectable PHP versions. (Runtime detection of
// installed versions is a follow-up; this is the allowlist for now.)
var SupportedVersions = []string{"8.1", "8.2", "8.3"}

// IsSupported reports whether v is a selectable version.
func IsSupported(v string) bool {
	for _, s := range SupportedVersions {
		if s == v {
			return true
		}
	}
	return false
}

// SocketPath is the deterministic FPM socket path for a site's Linux user. The
// web-server vhost and the FPM pool both derive from it, so no lookup is needed
// to wire them together.
func SocketPath(user string) string { return "/run/heropanel/fpm/" + user + ".sock" }

// PoolRecord is the persisted per-site pool configuration.
type PoolRecord struct {
	ID            int64  `db:"id"`
	SiteID        int64  `db:"site_id"`
	PHPVersion    string `db:"php_version"`
	PM            string `db:"pm"`
	MaxChildren   int    `db:"pm_max_children"`
	MemoryLimitMB int    `db:"memory_limit_mb"`
	SocketPath    string `db:"socket_path"`
}

// PoolRepo is the persistence contract (implemented by internal/repository).
type PoolRepo interface {
	Upsert(ctx context.Context, r *PoolRecord) error
	GetBySiteID(ctx context.Context, siteID int64) (*PoolRecord, error)
}

// PoolRequest asks the service to create or update a site's FPM pool.
type PoolRequest struct {
	SiteID       int64
	User         string
	Home         string
	DocumentRoot string
	Version      string // "" => DefaultVersion
}

// Manager is the interface the site service depends on.
type Manager interface {
	EnsurePool(ctx context.Context, req PoolRequest) (*PoolRecord, error)
	GetBySiteID(ctx context.Context, siteID int64) (*PoolRecord, error)
}

// Service manages FPM pools via the broker and persists their config.
type Service struct {
	repo   PoolRepo
	broker broker.Gateway
}

// NewService constructs the PHP Service.
func NewService(repo PoolRepo, gw broker.Gateway) *Service { return &Service{repo: repo, broker: gw} }

var _ Manager = (*Service)(nil)

// pool render inputs.
type poolTmplData struct {
	PoolName      string
	User          string
	Socket        string
	PM            string
	MaxChildren   int
	MemoryLimitMB int
	Home          string
	DocumentRoot  string
}

// EnsurePool creates or updates the site's FPM pool for the requested version
// (writing it via the broker) and persists the pool record.
func (s *Service) EnsurePool(ctx context.Context, req PoolRequest) (*PoolRecord, error) {
	version := req.Version
	if version == "" {
		version = DefaultVersion
	}
	if !IsSupported(version) {
		return nil, errx.Validation("unsupported_php_version", "That PHP version is not available.",
			errx.Field{Field: "version", Code: "unsupported", Message: "unsupported version"})
	}
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; PHP pools cannot be configured.")
	}

	const (
		pm       = "ondemand"
		children = 10
		memMB    = 256
	)
	socket := SocketPath(req.User)
	cfg, err := renderPool(poolTmplData{
		PoolName: req.User, User: req.User, Socket: socket, PM: pm,
		MaxChildren: children, MemoryLimitMB: memMB, Home: req.Home, DocumentRoot: req.DocumentRoot,
	})
	if err != nil {
		return nil, err
	}

	if _, err := s.broker.Invoke(ctx, "php.write_pool", map[string]any{
		"version":   version,
		"pool_name": req.User,
		"config":    cfg,
	}); err != nil {
		return nil, err
	}

	rec := &PoolRecord{
		SiteID: req.SiteID, PHPVersion: version, PM: pm,
		MaxChildren: children, MemoryLimitMB: memMB, SocketPath: socket,
	}
	if err := s.repo.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// GetBySiteID returns a site's pool, or a not-found error.
func (s *Service) GetBySiteID(ctx context.Context, siteID int64) (*PoolRecord, error) {
	return s.repo.GetBySiteID(ctx, siteID)
}

var poolTmpl = template.Must(template.New("pool").Parse(`[{{.PoolName}}]
user = {{.User}}
group = {{.User}}
listen = {{.Socket}}
listen.owner = {{.User}}
listen.group = {{.User}}
listen.mode = 0660
pm = {{.PM}}
pm.max_children = {{.MaxChildren}}
pm.process_idle_timeout = 10s
pm.max_requests = 500
php_admin_value[memory_limit] = {{.MemoryLimitMB}}M
php_admin_value[open_basedir] = {{.Home}}/:/tmp/
php_admin_value[upload_tmp_dir] = {{.Home}}/tmp
php_admin_value[session.save_path] = {{.Home}}/sessions
chdir = {{.DocumentRoot}}
`))

func renderPool(d poolTmplData) (string, error) {
	var b bytes.Buffer
	if err := poolTmpl.Execute(&b, d); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "pool_render_failed", "Could not render the PHP pool config.")
	}
	return b.String(), nil
}
