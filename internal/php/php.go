// Package php manages per-site PHP-FPM pools and the PHP version selector. Each
// PHP site gets a dedicated FPM pool listening on a private Unix socket, running
// as the site's Linux user with open_basedir confinement (docs/05 §3). hpd
// renders the pool config; the broker writes it and reloads php-fpm.
package php

import (
	"bytes"
	"context"
	"encoding/json"
	"text/template"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// DefaultVersion is used when a PHP site does not specify a version.
const DefaultVersion = "8.3"

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

// FpmBinary returns the php-fpm binary path for a version (Debian/Ubuntu layout).
// OpenLiteSpeed requires a path on the fcgi extProcessor even for an externally-
// managed pool (autoStart 0), so this is passed through to the vhost render.
func FpmBinary(version string) string { return "/usr/sbin/php-fpm" + version }

// PoolRecord is the persisted per-site pool configuration.
type PoolRecord struct {
	ID              int64  `db:"id"`
	SiteID          int64  `db:"site_id"`
	PHPVersion      string `db:"php_version"`
	PM              string `db:"pm"`
	MaxChildren     int    `db:"pm_max_children"`
	StartServers    int    `db:"pm_start_servers"`
	MinSpareServers int    `db:"pm_min_spare_servers"`
	MaxSpareServers int    `db:"pm_max_spare_servers"`
	MaxRequests     int    `db:"pm_max_requests"`
	IdleTimeoutSec  int    `db:"pm_idle_timeout_sec"`
	MemoryLimitMB   int    `db:"memory_limit_mb"`
	INIOverrides    string `db:"ini_overrides"` // JSON object
	OPcacheEnabled  bool   `db:"opcache_enabled"`
	OPcacheJIT      string `db:"opcache_jit"`
	SocketPath      string `db:"socket_path"`
}

// SettingsOf reads a record back into the settings envelope.
//
// A row whose ini_overrides will not parse yields an empty map rather than an
// error. The stored JSON is written only by this service from an allowlisted
// map, so unparseable means the column was tampered with or migrated oddly —
// and in either case refusing to show the operator their pool at all would be
// the less useful failure.
func SettingsOf(r *PoolRecord) Settings {
	s := Settings{
		Version:       r.PHPVersion,
		MemoryLimitMB: r.MemoryLimitMB,
		FPM: FPM{
			PM: r.PM, MaxChildren: r.MaxChildren, StartServers: r.StartServers,
			MinSpareServers: r.MinSpareServers, MaxSpareServers: r.MaxSpareServers,
			MaxRequests: r.MaxRequests, IdleTimeoutSec: r.IdleTimeoutSec,
		},
		INI:     map[string]string{},
		OPcache: OPcache{Enabled: r.OPcacheEnabled, JIT: r.OPcacheJIT},
	}
	if r.INIOverrides != "" {
		_ = json.Unmarshal([]byte(r.INIOverrides), &s.INI)
	}
	if s.INI == nil {
		s.INI = map[string]string{}
	}
	if s.OPcache.JIT == "" {
		s.OPcache.JIT = JITOff
	}
	return s
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
	ApplySettings(ctx context.Context, req PoolRequest, settings Settings) (*PoolRecord, error)
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
	PoolName        string
	User            string
	Socket          string
	PM              string
	MaxChildren     int
	StartServers    int
	MinSpareServers int
	MaxSpareServers int
	MaxRequests     int
	IdleTimeoutSec  int
	MemoryLimitMB   int
	INI             []iniPair
	OPcacheEnabled  bool
	OPcacheJIT      string
	Home            string
	DocumentRoot    string
}

// poolData turns validated settings into template input.
func poolData(req PoolRequest, s Settings) poolTmplData {
	return poolTmplData{
		PoolName: req.User, User: req.User, Socket: SocketPath(req.User),
		PM:              s.FPM.PM,
		MaxChildren:     s.FPM.MaxChildren,
		StartServers:    s.FPM.StartServers,
		MinSpareServers: s.FPM.MinSpareServers,
		MaxSpareServers: s.FPM.MaxSpareServers,
		MaxRequests:     s.FPM.MaxRequests,
		IdleTimeoutSec:  s.FPM.IdleTimeoutSec,
		MemoryLimitMB:   s.MemoryLimitMB,
		INI:             sortedINI(s.INI),
		OPcacheEnabled:  s.OPcache.Enabled,
		OPcacheJIT:      jitDirective(s.OPcache.JIT),
		Home:            req.Home, DocumentRoot: req.DocumentRoot,
	}
}

// EnsurePool creates or updates the site's FPM pool, keeping whatever settings
// the site already has and changing only the version.
//
// Reading the existing row first is what makes the PHP selector safe to use: a
// site tuned to 40 workers and 1G must not be quietly reset to the defaults
// because someone moved it from 8.2 to 8.3.
func (s *Service) EnsurePool(ctx context.Context, req PoolRequest) (*PoolRecord, error) {
	settings := DefaultSettings()
	if existing, err := s.repo.GetBySiteID(ctx, req.SiteID); err == nil && existing != nil {
		settings = SettingsOf(existing)
	}
	if req.Version != "" {
		settings.Version = req.Version
	}
	return s.ApplySettings(ctx, req, settings)
}

// ApplySettings validates the envelope, writes the pool through the broker, and
// records it.
//
// The broker goes first, and that ordering is the whole safety story: it writes
// the file, config-tests it, and rolls back if php-fpm refuses. Recording a row
// the running server rejected would have the panel report a configuration that
// does not exist — and for PHP that is worse than usual, because a bad pool file
// takes down every site sharing the version, not just this one.
func (s *Service) ApplySettings(ctx context.Context, req PoolRequest, settings Settings) (*PoolRecord, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; PHP pools cannot be configured.")
	}

	cfg, err := renderPool(poolData(req, settings))
	if err != nil {
		return nil, err
	}
	if _, err := s.broker.Invoke(ctx, "php.write_pool", map[string]any{
		"version":   settings.Version,
		"pool_name": req.User,
		"config":    cfg,
	}); err != nil {
		return nil, err
	}

	iniJSON, err := json.Marshal(settings.INI)
	if err != nil {
		return nil, errx.Wrap(err, errx.KindInternal, "ini_encode_failed", "Could not store the php.ini overrides.")
	}
	rec := &PoolRecord{
		SiteID:          req.SiteID,
		PHPVersion:      settings.Version,
		PM:              settings.FPM.PM,
		MaxChildren:     settings.FPM.MaxChildren,
		StartServers:    settings.FPM.StartServers,
		MinSpareServers: settings.FPM.MinSpareServers,
		MaxSpareServers: settings.FPM.MaxSpareServers,
		MaxRequests:     settings.FPM.MaxRequests,
		IdleTimeoutSec:  settings.FPM.IdleTimeoutSec,
		MemoryLimitMB:   settings.MemoryLimitMB,
		INIOverrides:    string(iniJSON),
		OPcacheEnabled:  settings.OPcache.Enabled,
		OPcacheJIT:      settings.OPcache.JIT,
		SocketPath:      SocketPath(req.User),
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

// poolTmpl renders a site's FPM pool.
//
// Order is load-bearing. A pool file is last-one-wins, so the operator's
// overrides are emitted first and the panel's confinement afterwards. Even if a
// directive somehow slipped past the allowlist in settings.go, it cannot loosen
// open_basedir or hand disable_functions back: the panel's line comes last and
// wins. Two independent guards, because one of them failing is how a tenant ends
// up reading every other site on the node.
var poolTmpl = template.Must(template.New("pool").Parse(`[{{.PoolName}}]
user = {{.User}}
group = {{.User}}
listen = {{.Socket}}
listen.owner = {{.User}}
listen.group = {{.User}}
listen.mode = 0660

; ── process manager ────────────────────────────────────────────────────────
pm = {{.PM}}
pm.max_children = {{.MaxChildren}}
{{- if eq .PM "dynamic"}}
pm.start_servers = {{.StartServers}}
pm.min_spare_servers = {{.MinSpareServers}}
pm.max_spare_servers = {{.MaxSpareServers}}
{{- end}}
{{- if eq .PM "ondemand"}}
pm.process_idle_timeout = {{.IdleTimeoutSec}}s
{{- end}}
pm.max_requests = {{.MaxRequests}}

; ── operator settings (php.ini editor, allowlisted) ────────────────────────
{{- range .INI}}
php_admin_value[{{.Key}}] = {{.Value}}
{{- end}}

; ── OPcache ────────────────────────────────────────────────────────────────
php_admin_value[opcache.enable] = {{if .OPcacheEnabled}}1{{else}}0{{end}}
{{- if .OPcacheJIT}}
php_admin_value[opcache.jit] = {{.OPcacheJIT}}
{{- end}}

; ── confinement: emitted last so it wins, whatever came before ─────────────
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
