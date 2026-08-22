// Package webmail integrates Roundcube as a panel-managed webmail client,
// served by the panel's own OpenLiteSpeed + PHP against the local Dovecot and
// Postfix (submission) — the same host that carries the mailboxes. It is not a
// customer site: it renders as a *system vhost* into the one OLS config, with a
// dedicated Linux user and FPM pool, on a configured webmail hostname. Roundcube
// authenticates each user against Dovecot at login, so the panel never handles a
// mailbox password; it only wires the client to the local MTAs over TLS.
package webmail

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"

	"github.com/thisisnkp/nexpanel/internal/broker"
	"github.com/thisisnkp/nexpanel/internal/php"
	"github.com/thisisnkp/nexpanel/internal/webserver"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Pinned paths, shared conceptually with the broker capability. The app tree is
// read-only; the data tree (sqlite db, temp, logs) is writable by the webmail
// user. The vhost derives from these deterministically — no stored state.
const (
	// vhostUser is the dedicated Linux identity Roundcube's FPM pool runs as.
	vhostUser = "webmail"
	// AppDir holds the read-only Roundcube application files.
	AppDir = "/usr/share/nexpanel/roundcube"
	// DataDir is the writable tree (temp, logs, roundcube.db).
	DataDir = "/var/lib/nexpanel/webmail"
	logDir  = DataDir + "/logs"
)

// VhostReloader re-renders and applies the OLS config so a newly installed
// webmail vhost starts serving. Implemented by the site service.
type VhostReloader interface {
	ReapplyWebserver(ctx context.Context) error
}

// Status is the API view of the webmail install.
type Status struct {
	Enabled   bool   `json:"enabled"`   // a hostname is configured
	Installed bool   `json:"installed"` // Roundcube is present and configured
	Hostname  string `json:"hostname"`
	URL       string `json:"url,omitempty"`
}

// Service manages the webmail install.
type Service struct {
	broker   broker.Gateway
	reloader VhostReloader
	hostname string // webmail FQDN (NP_WEBMAIL_HOSTNAME); "" = disabled
	phpVer   string // php-fpm version for the webmail pool (e.g. "8.4")
}

// NewService constructs the webmail service. hostname empty leaves it disabled.
func NewService(gw broker.Gateway, hostname, phpVersion string) *Service {
	// Falls through to the panel's own default rather than a second constant:
	// two hard-coded versions drift, and the one that drifts is this one,
	// because nobody looks at webmail until it breaks.
	if phpVersion == "" {
		phpVersion = php.DefaultVersion
	}
	return &Service{broker: gw, hostname: strings.ToLower(strings.TrimSpace(hostname)), phpVer: phpVersion}
}

// WithReloader wires the site service so Install can re-apply the OLS config.
func (s *Service) WithReloader(r VhostReloader) *Service { s.reloader = r; return s }

// Available reports whether webmail can operate (broker + a hostname).
func (s *Service) Available() bool {
	return s != nil && s.broker != nil && s.hostname != ""
}

// Enabled reports whether a hostname is configured.
func (s *Service) Enabled() bool { return s != nil && s.hostname != "" }

// Install lays down Roundcube's FPM pool + config against the local MTAs and
// re-applies the web server so the webmail vhost starts serving. Idempotent:
// re-running rewrites the same pinned configuration.
func (s *Service) Install(ctx context.Context) (*Status, error) {
	if !s.Available() {
		return nil, errx.New(errx.KindUnavailable, "webmail_unavailable",
			"Webmail needs the broker and a hostname (NP_WEBMAIL_HOSTNAME).")
	}
	desKey, err := desKey()
	if err != nil {
		return nil, errx.Internal(err)
	}
	cfg := RenderConfig(Config{
		IMAPHost: "tls://127.0.0.1", IMAPPort: 143,
		SMTPHost: "tls://127.0.0.1", SMTPPort: 587,
		DESKey:  desKey,
		DBPath:  DataDir + "/roundcube.db",
		TempDir: DataDir + "/temp",
		LogDir:  logDir,
		// A real cert for the mail host verifies normally; the self-signed
		// fallback on the loopback does not, so tolerate it there.
		SkipCertVerify: true,
		ProductName:    "NexPanel Webmail",
	})
	if _, err := s.broker.Invoke(ctx, "webmail.install", map[string]any{
		"config": cfg,
	}); err != nil {
		return nil, err
	}
	// The dedicated FPM pool Roundcube runs in — written and config-tested
	// through the same php.write_pool capability every site pool uses.
	if _, err := s.broker.Invoke(ctx, "php.write_pool", map[string]any{
		"version":   s.phpVer,
		"pool_name": vhostUser,
		"config":    renderPool(s.phpVer),
	}); err != nil {
		return nil, err
	}
	// Make the vhost live now, rather than waiting for the next site change.
	if s.reloader != nil {
		if err := s.reloader.ReapplyWebserver(ctx); err != nil {
			return nil, err
		}
	}
	return s.Status(ctx)
}

// Status reports whether Roundcube is installed (the broker checks disk) and
// the webmail URL.
func (s *Service) Status(ctx context.Context) (*Status, error) {
	st := &Status{Enabled: s.Enabled(), Hostname: s.hostname}
	if !s.Enabled() {
		return st, nil
	}
	st.URL = "https://" + s.hostname + "/"
	if s.broker != nil {
		if out, err := s.broker.Invoke(ctx, "webmail.status", map[string]any{}); err == nil {
			if v, ok := out["installed"].(bool); ok {
				st.Installed = v
			}
		}
	}
	return st, nil
}

// SystemVhosts returns the webmail vhost for the site render. It is contributed
// whenever a hostname is configured; an unstarted FPM pool simply means the
// vhost 502s until Install runs, which is a truer signal than the vhost being
// absent.
func (s *Service) SystemVhosts(_ context.Context) []webserver.Site {
	if !s.Enabled() {
		return nil
	}
	return []webserver.Site{{
		VhostName:     vhostUser,
		PrimaryDomain: s.hostname,
		Domains:       []string{s.hostname},
		// vhRoot and docRoot are both the app dir: OpenLiteSpeed renders every
		// vhost `restrained`, so the document root must sit under the vhost root.
		// The writable data tree lives outside it and is reached only through the
		// FPM pool's open_basedir, not through the web root.
		DocumentRoot: AppDir,
		Home:         AppDir,
		LogDir:       logDir,
		IsPHP:        true,
		FpmSocket:    php.SocketPath(vhostUser),
		PhpBin:       php.FpmBinary(s.phpVer),
	}}
}

// renderPool renders the webmail PHP-FPM pool. It mirrors the per-site pool
// (dedicated user, private socket, open_basedir confinement) but is confined to
// the read-only app tree plus the writable data tree — Roundcube can read its
// code and write its temp/logs/db, and nothing else.
func renderPool(_ string) string {
	sock := php.SocketPath(vhostUser)
	return "[" + vhostUser + "]\n" +
		"user = " + vhostUser + "\n" +
		"group = " + vhostUser + "\n" +
		"listen = " + sock + "\n" +
		"listen.owner = " + vhostUser + "\n" +
		"listen.group = " + vhostUser + "\n" +
		"listen.mode = 0660\n" +
		"pm = ondemand\n" +
		"pm.max_children = 10\n" +
		"pm.process_idle_timeout = 30s\n" +
		"pm.max_requests = 500\n" +
		"php_admin_value[memory_limit] = 256M\n" +
		"php_admin_value[open_basedir] = " + AppDir + "/:" + DataDir + "/:/tmp/\n" +
		"php_admin_value[upload_tmp_dir] = " + DataDir + "/temp\n" +
		"php_admin_value[session.save_path] = " + DataDir + "/temp\n" +
		"chdir = " + AppDir + "\n"
}

// desKey generates a 24-character Roundcube DES key (base64 of 18 bytes).
func desKey() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
