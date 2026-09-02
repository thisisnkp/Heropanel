// Package config loads NexPanel's layered configuration:
// compiled defaults -> /etc/nexpanel/config.yaml -> NP_* environment vars.
// Later layers override earlier ones. Secrets are expected via env or a
// separate secrets file, never committed to the YAML. See docs/01 §5.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the full npd configuration.
type Config struct {
	Server      Server      `yaml:"server"`
	Database    Database    `yaml:"database"`
	Redis       Redis       `yaml:"redis"`
	Broker      Broker      `yaml:"broker"`
	SSL         SSL         `yaml:"ssl"`
	Terminal    Terminal    `yaml:"terminal"`
	Log         Log         `yaml:"log"`
	Security    Security    `yaml:"security"`
	Backup      Backup      `yaml:"backup"`
	Mail        Mail        `yaml:"mail"`
	Webmail     Webmail     `yaml:"webmail"`
	Marketplace Marketplace `yaml:"marketplace"`
	Update      Update      `yaml:"update"`
	License     License     `yaml:"license"`
}

// License points the panel at its licence server (docs/27).
//
// PubKey is the ed25519 key licence tokens are verified against — and unlike
// every other key in this file it is normally **not** read from here. Official
// builds compile it in, and a build that pins a key ignores this setting
// entirely: a verification key an operator can edit verifies nothing, and on a
// customer's own VPS the operator is the customer. It exists for development
// and for anyone running their own licence server from source.
//
// ServerURL has a default because a panel that cannot find its licence server
// is a panel nobody can activate; KeyID names which pinned key the server is
// currently signing with, and is only a hint — the token's own header decides.
type License struct {
	ServerURL string `yaml:"server_url"`
	KeyID     string `yaml:"key_id"`
	PubKey    string `yaml:"pubkey"`
}

// Update configures panel self-update (docs/26). BaseURL is the release root a
// channel manifest and its artifacts are published under; Channel selects which
// line this installation follows. PubKey is the ed25519 **release** key — the
// same anchor np-installer pins as NP_RELEASE_PUBKEY, because the update path
// verifies the identical SHA256SUMS chain the installer does; a public key, so
// yaml is fine.
//
// Every field is optional and the feature is off unless BaseURL *and* PubKey
// are both set: an update source with no anchor would let whoever serves that
// URL replace the root component on this host, which is the one thing this
// design exists to prevent. AutoCheck only ever *checks* — nothing installs
// itself without an operator pressing the button.
type Update struct {
	Channel   string `yaml:"channel"`    // stable | beta | nightly
	BaseURL   string `yaml:"base_url"`   // e.g. https://releases.nexpanel.io
	PubKey    string `yaml:"pubkey"`     // base64 / hex / @path ed25519 public key
	AutoCheck bool   `yaml:"auto_check"` // poll for a newer release in the background
}

// Marketplace configures the module marketplace. Catalog is a path to the module
// feed (a JSON index of signed manifests); empty leaves the catalog empty. Keys
// are the ed25519 publisher public keys the panel pins to decide which modules it
// trusts to install — base64, hex, or a "@/path/to/key" reference. These are
// public keys, so unlike credentials they may live in the yaml file;
// NP_MARKETPLACE_KEYS (comma-separated) and NP_MARKETPLACE_CATALOG override them.
type Marketplace struct {
	Catalog string   `yaml:"catalog"`
	Keys    []string `yaml:"keys"`
}

// Webmail configures the Roundcube webmail integration. Hostname is the FQDN it
// is served on (webmail.example.com); empty disables webmail. PHPVersion is the
// php-fpm version its pool runs (defaults to the panel's own default version).
type Webmail struct {
	Hostname   string `yaml:"hostname"`
	PHPVersion string `yaml:"php_version"`
}

// Mail configures the mail module's edges. Resolver pins the DNS server the
// live record check queries (host:port) — for split-DNS setups and e2e against
// a local authoritative server; empty uses the system resolver.
type Mail struct {
	Resolver string `yaml:"resolver"`
	// Hostname is the mail server's own FQDN (mail.example.com). The MTAs
	// present this host's single certificate on submission/587, imaps/993 and
	// smtps/465. Empty leaves TLS off (delivery on port 25 still works).
	Hostname string `yaml:"hostname"`
}

// Backup configures where sealed site backups may be sent besides local disk.
// All-empty means "local only", which always works.
type Backup struct {
	S3     BackupS3     `yaml:"s3"`
	SFTP   BackupSFTP   `yaml:"sftp"`
	Rclone BackupRclone `yaml:"rclone"`
	Panel  BackupPanel  `yaml:"panel"`
	// SweepIntervalSec is how often the backup schedulers re-check for due
	// backups. 0 = the default (one hour). Small values are for e2e only:
	// due-ness is still governed by each policy's interval_hours.
	SweepIntervalSec int `yaml:"sweep_interval_sec"`
}

// BackupSFTP is an SFTP (SSH) backup target. Password/PrivateKey are secrets and
// come from the environment, never the yaml file. HostKey pins the server's
// public key (an authorized_keys line) — strongly recommended; empty means the
// host key is not verified.
type BackupSFTP struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	User       string `yaml:"user"`
	BasePath   string `yaml:"base_path"`
	HostKey    string `yaml:"host_key"`
	Password   string `yaml:"-"`
	PrivateKey string `yaml:"-"`
}

// BackupRclone is an rclone-backed target: any of rclone's cloud backends
// (Google Drive, Dropbox, OneDrive, …). The operator configures the remote with
// `rclone config`; the panel streams sealed blobs to it. Remote empty = off.
type BackupRclone struct {
	Bin    string `yaml:"bin"`    // rclone binary (default "rclone")
	Config string `yaml:"config"` // path to rclone.conf ("" = rclone default)
	Remote string `yaml:"remote"` // e.g. "gdrive:nexpanel-backups"
}

// BackupPanel drives the panel's self-backup: a sealed snapshot of the panel's
// own database on a schedule. Enabled by default (it costs a few MB and is the
// difference between a bad day and a disaster) — it still only runs when
// NP_SECRET_KEY is set, because sealed-at-rest is not optional.
type BackupPanel struct {
	Enabled       bool   `yaml:"enabled"`
	IntervalHours int    `yaml:"interval_hours"`
	Target        string `yaml:"target"` // local | s3
	Keep          int    `yaml:"keep"`
}

// BackupS3 is an S3-compatible target (AWS, R2, B2, MinIO). The secret key may
// come from NP_BACKUP_S3_SECRET_KEY rather than the file.
type BackupS3 struct {
	Endpoint  string `yaml:"endpoint"`
	Region    string `yaml:"region"`
	Bucket    string `yaml:"bucket"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"-"`
}

// Terminal configures the web terminal and its session recording.
type Terminal struct {
	Recording Recording `yaml:"recording"`
	// IdleTimeout closes an interactive session after this long with no activity
	// in either direction (no keystrokes and no output). 0 disables the timeout.
	// A dangling shell on a customer's site is a standing risk; this bounds it.
	IdleTimeout Duration `yaml:"idle_timeout"`
}

// Recording configures session recording. An empty Dir switches it off: the
// terminal still works, sessions are simply not recorded.
//
// Recordings capture keystrokes as well as output, so input typed while the
// terminal had echo disabled — a password prompt — is redacted before it is
// written. See internal/terminal/recording.go.
type Recording struct {
	Dir           string `yaml:"dir"`
	RetentionDays int    `yaml:"retention_days"`
}

// Broker configures the connection to the privileged np-broker daemon. An empty
// Socket disables the connection (npd runs without privileged operations).
//
// Remote is the multi-node case (docs/27): the broker being driven runs on
// another host, so the Unix socket's kernel-attested peer credentials are not
// available and a client certificate takes their place. Remote and Socket are
// alternatives, not layers — a panel talks to one broker.
type Broker struct {
	Socket string       `yaml:"socket"`
	Token  string       `yaml:"token"`
	Remote BrokerRemote `yaml:"remote"`
}

// BrokerRemote points npd at a broker on another node over mutual TLS. It is
// off unless Addr is set, and incomplete settings are a startup error rather
// than a silent fallback to something less authenticated.
type BrokerRemote struct {
	// Addr is host:port of the remote broker's TLS listener.
	Addr string `yaml:"addr"`
	// ServerName must match a SAN on the broker's certificate. Defaults to the
	// host part of Addr when empty.
	ServerName string `yaml:"server_name"`
	// CAFile verifies the broker; CertFile/KeyFile are this node's own identity.
	// Paths rather than inline PEM: a private key does not belong in a config
	// file that gets copied around, and the installer already manages file modes.
	CAFile   string `yaml:"ca_file"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Enabled reports whether a remote broker is configured.
func (r BrokerRemote) Enabled() bool { return r.Addr != "" }

// Validate refuses a half-configured remote broker.
func (r BrokerRemote) Validate() error {
	if !r.Enabled() {
		return nil
	}
	switch {
	case r.CAFile == "":
		return errors.New("broker.remote.ca_file is required (there is no way to verify the broker without it)")
	case r.CertFile == "" || r.KeyFile == "":
		return errors.New("broker.remote.cert_file and key_file are required (the broker requires a client certificate)")
	}
	return nil
}

// EffectiveServerName is the name the broker's certificate must carry.
func (r BrokerRemote) EffectiveServerName() string {
	if r.ServerName != "" {
		return r.ServerName
	}
	if host, _, err := net.SplitHostPort(r.Addr); err == nil {
		return host
	}
	return r.Addr
}

// SSL configures ACME (Let's Encrypt). Self-signed and custom uploads work
// without it; ACME issuance requires an account Email. Directory defaults to
// production Let's Encrypt when empty.
type SSL struct {
	Email     string `yaml:"email"`
	Directory string `yaml:"directory"`
	// ZeroSSL is an optional second ACME CA. It requires External Account
	// Binding: ZeroSSLEABKID from the yaml/env, and the HMAC key from the secret
	// env only (NP_SSL_ZEROSSL_EAB_HMAC) — never the yaml file. ZeroSSLDirectory
	// defaults to ZeroSSL production when empty but EAB is set.
	ZeroSSLDirectory string `yaml:"zerossl_directory"`
	ZeroSSLEABKID    string `yaml:"zerossl_eab_kid"`
	ZeroSSLEABHMAC   string `yaml:"-"` // secret env only
}

// Server holds HTTP server settings.
type Server struct {
	Host            string   `yaml:"host"`
	Port            int      `yaml:"port"`
	ReadTimeout     Duration `yaml:"read_timeout"`
	WriteTimeout    Duration `yaml:"write_timeout"`
	IdleTimeout     Duration `yaml:"idle_timeout"`
	ShutdownTimeout Duration `yaml:"shutdown_timeout"`
	TLS             TLS      `yaml:"tls"`
}

// TLS configures the panel's own HTTPS listener.
type TLS struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Database configures the control-plane datastore.
type Database struct {
	Driver          string   `yaml:"driver"` // mariadb | sqlite
	DSN             string   `yaml:"dsn"`
	MaxOpenConns    int      `yaml:"max_open_conns"`
	MaxIdleConns    int      `yaml:"max_idle_conns"`
	ConnMaxLifetime Duration `yaml:"conn_max_lifetime"`
	// AdminerURL is where Adminer (or phpMyAdmin) is served. Setting it enables
	// the one-click hand-off, which signs in with a throwaway account rather than
	// a stored password. Empty disables the hand-off.
	AdminerURL string `yaml:"adminer_url"`
}

// Redis configures the cache/queue/bus.
type Redis struct {
	Addr     string `yaml:"addr"`
	DB       int    `yaml:"db"`
	Password string `yaml:"password"`
}

// Log configures structured logging.
type Log struct {
	Level  string `yaml:"level"`  // debug|info|warn|error
	Format string `yaml:"format"` // json|text
}

// Security groups edge protections.
type Security struct {
	BodyLimitBytes int64     `yaml:"body_limit_bytes"`
	RateLimit      RateLimit `yaml:"rate_limit"`
	CORS           CORS      `yaml:"cors"`
	CSRF           CSRF      `yaml:"csrf"`
	// SecretKey is the base64-encoded 32-byte master key that encrypts the *_enc
	// columns (Git credentials today). Supply it via NP_SECRET_KEY or the
	// secrets.env file, never in config.yaml. Empty disables features that must
	// store a secret at rest — they report "unavailable" rather than falling back
	// to plaintext storage.
	SecretKey string `yaml:"-"`
	// PanelIPAllowlist restricts panel/API access to these CIDRs (or bare IPs).
	// Empty = open to all (the default). A misconfigured allowlist can lock the
	// operator out, so it is opt-in and set deliberately. NP_PANEL_IP_ALLOWLIST
	// is a comma-separated override.
	PanelIPAllowlist []string `yaml:"panel_ip_allowlist"`
	// WebAuthn configures passkeys. Empty RPID keeps them disabled — the
	// relying-party id must match the panel's domain exactly and cannot be
	// guessed safely.
	WebAuthn WebAuthn `yaml:"webauthn"`
	// FirewallWindowSec overrides the firewall confirmation window (0 = the
	// module default of 60s). Bounded by the module's own floor.
	FirewallWindowSec int `yaml:"firewall_window_sec"`
	// GeoDBURLv4/v6 are URL templates (each with a single %s for the lowercase
	// ISO country code) for the aggregated-CIDR zone files a country geo-import
	// fetches. They default to the public ipdeny aggregated mirrors; point them
	// at an internal copy to keep the panel's outbound reach in your control.
	// Overridable via NP_SECURITY_GEODB_URL / NP_SECURITY_GEODB_URL6.
	GeoDBURLv4 string `yaml:"geodb_url_v4"`
	GeoDBURLv6 string `yaml:"geodb_url_v6"`
	// MaldetPath is the tarball path on rfxn.com (the host is pinned in the
	// broker and is not configurable — an operator-supplied URL would turn this
	// file into "run this as root"). Empty uses the current release.
	MaldetPath string `yaml:"maldet_path"`
	// MaldetSHA256 pins the maldet tarball's checksum. Empty means the install
	// is verified only by TLS and the pinned host, which is all maldet's
	// distribution offers — rfxn publishes no signature and no stable checksum.
	// The panel reports the hash it actually installed so this can be filled in
	// afterwards and every later install checked against it.
	// Overridable via NP_SECURITY_MALDET_PATH / NP_SECURITY_MALDET_SHA256.
	MaldetSHA256 string `yaml:"maldet_sha256"`
}

// WebAuthn identifies the relying party for passkeys.
type WebAuthn struct {
	RPID   string `yaml:"rp_id"`   // the panel's registrable domain, e.g. panel.example.com
	RPName string `yaml:"rp_name"` // display name, e.g. "NexPanel"
	Origin string `yaml:"origin"`  // the full origin, e.g. https://panel.example.com
}

// CSRF configures double-submit CSRF protection for cookie-authenticated
// mutations. Disabled by default; SameSite=Strict cookies already mitigate CSRF,
// and enabling this requires clients to echo the np_csrf cookie in X-CSRF-Token.
type CSRF struct {
	Enabled bool `yaml:"enabled"`
}

// RateLimit configures the in-process per-IP limiter (Redis-backed distributed
// limiting is added when Redis is wired).
type RateLimit struct {
	Enabled bool    `yaml:"enabled"`
	RPS     float64 `yaml:"rps"`
	Burst   int     `yaml:"burst"`
}

// CORS lists origins permitted to call the API from a browser.
type CORS struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// Default returns the compiled-in defaults (a safe single-node baseline).
func Default() Config {
	return Config{
		Server: Server{
			Host:            "0.0.0.0",
			Port:            8443,
			ReadTimeout:     dur(15 * time.Second),
			WriteTimeout:    dur(30 * time.Second),
			IdleTimeout:     dur(60 * time.Second),
			ShutdownTimeout: dur(15 * time.Second),
		},
		Database: Database{
			Driver:          "mariadb",
			MaxOpenConns:    25,
			MaxIdleConns:    5,
			ConnMaxLifetime: dur(30 * time.Minute),
		},
		Redis: Redis{Addr: "", DB: 0}, // empty = disabled (opt-in, like the DB DSN)
		// Terminal sessions are recorded by default. A shell as a site's Linux
		// user is the most powerful thing the panel hands out, and defaulting the
		// audit trail to off means it is missing exactly when someone thinks to
		// look for it. Set terminal.recording.dir to "" to switch it off.
		Terminal: Terminal{Recording: Recording{
			Dir:           "/var/lib/nexpanel/recordings",
			RetentionDays: 30,
		}},
		Log: Log{Level: "info", Format: "json"},
		Security: Security{
			BodyLimitBytes: 10 << 20, // 10 MiB
			RateLimit:      RateLimit{Enabled: true, RPS: 20, Burst: 40},
			CORS:           CORS{AllowedOrigins: []string{}},
			GeoDBURLv4:     "https://www.ipdeny.com/ipblocks/data/aggregated/%s-aggregated.zone",
			GeoDBURLv6:     "https://www.ipdeny.com/ipv6/ipaddresses/aggregated/%s-aggregated.zone",
		},
		Backup: Backup{
			Panel: BackupPanel{Enabled: true, IntervalHours: 24, Target: "local", Keep: 7},
		},
		// Only the channel has a default. BaseURL and PubKey are left empty on
		// purpose — self-update stays off until an operator names both a release
		// source and the key that vouches for it.
		Update: Update{Channel: "stable"},
		// The licence server every official build talks to. Overridable for a
		// staging deployment or a self-hosted server; the *key* is what decides
		// whether a token is believed, so pointing this elsewhere without also
		// having that server's key produces tokens this panel refuses.
		License: License{ServerURL: "https://license.nexpanel.io", KeyID: "lk1"},
	}
}

// Load builds the effective config: defaults, then the YAML file at path (if
// path is non-empty), then NP_* environment overrides, then validation.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	cfg.applyEnv()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// applyEnv overlays a curated set of NP_* environment variables.
func (c *Config) applyEnv() {
	if v := os.Getenv("NP_SERVER_HOST"); v != "" {
		c.Server.Host = v
	}
	if v := os.Getenv("NP_SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Server.Port = p
		}
	}
	// The write timeout bounds a synchronous response. Long-running privileged
	// reads (a full rkhunter/lynis audit that takes minutes) need it raised, so
	// it is env-overridable (e.g. "600s").
	if v := os.Getenv("NP_TERMINAL_IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.Terminal.IdleTimeout = Duration(d)
		}
	}
	if v := os.Getenv("NP_SERVER_WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.Server.WriteTimeout = Duration(d)
		}
	}
	if v := os.Getenv("NP_LOG_LEVEL"); v != "" {
		c.Log.Level = v
	}
	if v := os.Getenv("NP_LOG_FORMAT"); v != "" {
		c.Log.Format = v
	}
	if v := os.Getenv("NP_DATABASE_DRIVER"); v != "" {
		c.Database.Driver = v
	}
	if v := os.Getenv("NP_DATABASE_DSN"); v != "" {
		c.Database.DSN = v
	}
	if v := os.Getenv("NP_BACKUP_S3_ENDPOINT"); v != "" {
		c.Backup.S3.Endpoint = v
	}
	if v := os.Getenv("NP_BACKUP_S3_REGION"); v != "" {
		c.Backup.S3.Region = v
	}
	if v := os.Getenv("NP_BACKUP_S3_BUCKET"); v != "" {
		c.Backup.S3.Bucket = v
	}
	if v := os.Getenv("NP_BACKUP_S3_ACCESS_KEY"); v != "" {
		c.Backup.S3.AccessKey = v
	}
	if v := os.Getenv("NP_BACKUP_S3_SECRET_KEY"); v != "" {
		c.Backup.S3.SecretKey = v
	}
	if v := os.Getenv("NP_BACKUP_SFTP_HOST"); v != "" {
		c.Backup.SFTP.Host = v
	}
	if v := os.Getenv("NP_BACKUP_SFTP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Backup.SFTP.Port = n
		}
	}
	if v := os.Getenv("NP_BACKUP_SFTP_USER"); v != "" {
		c.Backup.SFTP.User = v
	}
	if v := os.Getenv("NP_BACKUP_SFTP_BASE_PATH"); v != "" {
		c.Backup.SFTP.BasePath = v
	}
	if v := os.Getenv("NP_BACKUP_SFTP_HOST_KEY"); v != "" {
		c.Backup.SFTP.HostKey = v
	}
	if v := os.Getenv("NP_BACKUP_SFTP_PASSWORD"); v != "" {
		c.Backup.SFTP.Password = v
	}
	if v := os.Getenv("NP_BACKUP_SFTP_PRIVATE_KEY"); v != "" {
		c.Backup.SFTP.PrivateKey = v
	}
	if v := os.Getenv("NP_BACKUP_RCLONE_REMOTE"); v != "" {
		c.Backup.Rclone.Remote = v
	}
	if v := os.Getenv("NP_BACKUP_RCLONE_CONFIG"); v != "" {
		c.Backup.Rclone.Config = v
	}
	if v := os.Getenv("NP_BACKUP_RCLONE_BIN"); v != "" {
		c.Backup.Rclone.Bin = v
	}
	if v := os.Getenv("NP_BACKUP_SWEEP_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Backup.SweepIntervalSec = n
		}
	}
	if v := os.Getenv("NP_BACKUP_PANEL_ENABLED"); v != "" {
		c.Backup.Panel.Enabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("NP_BACKUP_PANEL_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Backup.Panel.IntervalHours = n
		}
	}
	if v := os.Getenv("NP_BACKUP_PANEL_TARGET"); v != "" {
		c.Backup.Panel.Target = v
	}
	if v := os.Getenv("NP_BACKUP_PANEL_KEEP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Backup.Panel.Keep = n
		}
	}
	if v := os.Getenv("NP_MAIL_RESOLVER"); v != "" {
		c.Mail.Resolver = v
	}
	if v := os.Getenv("NP_MAIL_HOSTNAME"); v != "" {
		c.Mail.Hostname = v
	}
	if v := os.Getenv("NP_WEBMAIL_HOSTNAME"); v != "" {
		c.Webmail.Hostname = v
	}
	if v := os.Getenv("NP_WEBMAIL_PHP_VERSION"); v != "" {
		c.Webmail.PHPVersion = v
	}
	if v := os.Getenv("NP_MARKETPLACE_CATALOG"); v != "" {
		c.Marketplace.Catalog = v
	}
	if v := os.Getenv("NP_MARKETPLACE_KEYS"); v != "" {
		c.Marketplace.Keys = c.Marketplace.Keys[:0]
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.Marketplace.Keys = append(c.Marketplace.Keys, p)
			}
		}
	}
	if v := os.Getenv("NP_UPDATE_CHANNEL"); v != "" {
		c.Update.Channel = v
	}
	if v := os.Getenv("NP_UPDATE_BASE_URL"); v != "" {
		c.Update.BaseURL = v
	}
	// Deliberately the same variable np-installer and install.sh already pin:
	// the update path checks the identical SHA256SUMS chain, so a second name
	// for the same anchor would only invite the two drifting apart.
	if v := os.Getenv("NP_RELEASE_PUBKEY"); v != "" {
		c.Update.PubKey = v
	}
	if v := os.Getenv("NP_UPDATE_AUTO_CHECK"); v != "" {
		c.Update.AutoCheck = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("NP_LICENSE_SERVER"); v != "" {
		c.License.ServerURL = v
	}
	if v := os.Getenv("NP_LICENSE_KEY_ID"); v != "" {
		c.License.KeyID = v
	}
	// Honoured only by a build that pins no key of its own — see the License
	// type. Reading it here regardless keeps the precedence rules in one place
	// (env beats yaml) rather than splitting them across two packages.
	if v := os.Getenv("NP_LICENSE_PUBKEY"); v != "" {
		c.License.PubKey = v
	}
	if v := os.Getenv("NP_PANEL_IP_ALLOWLIST"); v != "" {
		parts := strings.Split(v, ",")
		c.Security.PanelIPAllowlist = c.Security.PanelIPAllowlist[:0]
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				c.Security.PanelIPAllowlist = append(c.Security.PanelIPAllowlist, p)
			}
		}
	}
	if v := os.Getenv("NP_WEBAUTHN_RP_ID"); v != "" {
		c.Security.WebAuthn.RPID = v
	}
	if v := os.Getenv("NP_WEBAUTHN_RP_NAME"); v != "" {
		c.Security.WebAuthn.RPName = v
	}
	if v := os.Getenv("NP_WEBAUTHN_ORIGIN"); v != "" {
		c.Security.WebAuthn.Origin = v
	}
	if v := os.Getenv("NP_FIREWALL_WINDOW_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Security.FirewallWindowSec = n
		}
	}
	if v := os.Getenv("NP_SECURITY_MALDET_PATH"); v != "" {
		c.Security.MaldetPath = v
	}
	if v := os.Getenv("NP_SECURITY_MALDET_SHA256"); v != "" {
		c.Security.MaldetSHA256 = v
	}
	if v := os.Getenv("NP_SECURITY_GEODB_URL"); v != "" {
		c.Security.GeoDBURLv4 = v
	}
	if v := os.Getenv("NP_SECURITY_GEODB_URL6"); v != "" {
		c.Security.GeoDBURLv6 = v
	}
	if v := os.Getenv("NP_DATABASE_ADMINER_URL"); v != "" {
		c.Database.AdminerURL = v
	}
	if v := os.Getenv("NP_REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := os.Getenv("NP_REDIS_PASSWORD"); v != "" {
		c.Redis.Password = v
	}
	if v := os.Getenv("NP_BROKER_SOCKET"); v != "" {
		c.Broker.Socket = v
	}
	if v := os.Getenv("NP_BROKER_TOKEN"); v != "" {
		c.Broker.Token = v
	}
	// Remote broker (docs/27). Only paths and an address here — the client's
	// private key stays a file on disk, never an environment variable.
	if v := os.Getenv("NP_BROKER_REMOTE_ADDR"); v != "" {
		c.Broker.Remote.Addr = v
	}
	if v := os.Getenv("NP_BROKER_REMOTE_SERVER_NAME"); v != "" {
		c.Broker.Remote.ServerName = v
	}
	if v := os.Getenv("NP_BROKER_REMOTE_CA_FILE"); v != "" {
		c.Broker.Remote.CAFile = v
	}
	if v := os.Getenv("NP_BROKER_REMOTE_CERT_FILE"); v != "" {
		c.Broker.Remote.CertFile = v
	}
	if v := os.Getenv("NP_BROKER_REMOTE_KEY_FILE"); v != "" {
		c.Broker.Remote.KeyFile = v
	}
	if v := os.Getenv("NP_SECRET_KEY"); v != "" {
		c.Security.SecretKey = v
	}
	// ACME (Let's Encrypt) account email and directory URL. Email enables
	// issuance; Directory points at a staging or test CA (e.g. Pebble) instead of
	// production — invaluable for testing against a real ACME server without
	// hitting Let's Encrypt's rate limits.
	if v := os.Getenv("NP_SSL_EMAIL"); v != "" {
		c.SSL.Email = v
	}
	if v := os.Getenv("NP_SSL_DIRECTORY"); v != "" {
		c.SSL.Directory = v
	}
	if v := os.Getenv("NP_SSL_ZEROSSL_DIRECTORY"); v != "" {
		c.SSL.ZeroSSLDirectory = v
	}
	if v := os.Getenv("NP_SSL_ZEROSSL_EAB_KID"); v != "" {
		c.SSL.ZeroSSLEABKID = v
	}
	// The EAB HMAC is a credential: secret env only, never the yaml.
	if v := os.Getenv("NP_SSL_ZEROSSL_EAB_HMAC"); v != "" {
		c.SSL.ZeroSSLEABHMAC = v
	}

	// The rate limiter, so a test harness or a load run can turn it off without
	// a config file. It protects a public panel from brute force; a browser
	// suite driving one instance single-threaded is not that, and being
	// throttled makes those runs flaky rather than safe.
	if v := os.Getenv("NP_SECURITY_RATE_LIMIT_ENABLED"); v != "" {
		c.Security.RateLimit.Enabled = !(v == "0" || strings.EqualFold(v, "false"))
	}

	// Terminal session recording. The directory is what switches it on.
	if v := os.Getenv("NP_TERMINAL_RECORDING_DIR"); v != "" {
		c.Terminal.Recording.Dir = v
	}
	if v := os.Getenv("NP_TERMINAL_RECORDING_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Terminal.Recording.RetentionDays = n
		}
	}
}

// Validate checks the effective config for obviously invalid values.
func (c Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("config: server.port %d out of range", c.Server.Port)
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("config: invalid log.level %q", c.Log.Level)
	}
	switch strings.ToLower(c.Log.Format) {
	case "json", "text":
	default:
		return fmt.Errorf("config: invalid log.format %q", c.Log.Format)
	}
	switch strings.ToLower(c.Database.Driver) {
	case "mariadb", "sqlite":
	default:
		return fmt.Errorf("config: invalid database.driver %q", c.Database.Driver)
	}
	if c.Server.TLS.Enabled && (c.Server.TLS.CertFile == "" || c.Server.TLS.KeyFile == "") {
		return fmt.Errorf("config: server.tls.enabled requires cert_file and key_file")
	}
	return nil
}

// ── Duration: a time.Duration that (un)marshals from human strings ("15s"). ──

// Duration wraps time.Duration so YAML can express "15s", "30m", etc.
type Duration time.Duration

func dur(d time.Duration) Duration { return Duration(d) }

// D returns the underlying time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalYAML accepts either a duration string ("15s") or a bare number of
// seconds.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("config: invalid duration %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}
	var secs int64
	if err := value.Decode(&secs); err != nil {
		return fmt.Errorf("config: duration must be a string or number of seconds")
	}
	*d = Duration(time.Duration(secs) * time.Second)
	return nil
}
