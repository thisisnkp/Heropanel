// Package config loads HeroPanel's layered configuration:
// compiled defaults -> /etc/heropanel/config.yaml -> HP_* environment vars.
// Later layers override earlier ones. Secrets are expected via env or a
// separate secrets file, never committed to the YAML. See docs/01 §5.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the full hpd configuration.
type Config struct {
	Server   Server   `yaml:"server"`
	Database Database `yaml:"database"`
	Redis    Redis    `yaml:"redis"`
	Broker   Broker   `yaml:"broker"`
	Log      Log      `yaml:"log"`
	Security Security `yaml:"security"`
}

// Broker configures the connection to the privileged hp-broker daemon. An empty
// Socket disables the connection (hpd runs without privileged operations).
type Broker struct {
	Socket string `yaml:"socket"`
	Token  string `yaml:"token"`
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
		Log:   Log{Level: "info", Format: "json"},
		Security: Security{
			BodyLimitBytes: 10 << 20, // 10 MiB
			RateLimit:      RateLimit{Enabled: true, RPS: 20, Burst: 40},
			CORS:           CORS{AllowedOrigins: []string{}},
		},
	}
}

// Load builds the effective config: defaults, then the YAML file at path (if
// path is non-empty), then HP_* environment overrides, then validation.
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

// applyEnv overlays a curated set of HP_* environment variables.
func (c *Config) applyEnv() {
	if v := os.Getenv("HP_SERVER_HOST"); v != "" {
		c.Server.Host = v
	}
	if v := os.Getenv("HP_SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Server.Port = p
		}
	}
	if v := os.Getenv("HP_LOG_LEVEL"); v != "" {
		c.Log.Level = v
	}
	if v := os.Getenv("HP_LOG_FORMAT"); v != "" {
		c.Log.Format = v
	}
	if v := os.Getenv("HP_DATABASE_DRIVER"); v != "" {
		c.Database.Driver = v
	}
	if v := os.Getenv("HP_DATABASE_DSN"); v != "" {
		c.Database.DSN = v
	}
	if v := os.Getenv("HP_REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := os.Getenv("HP_REDIS_PASSWORD"); v != "" {
		c.Redis.Password = v
	}
	if v := os.Getenv("HP_BROKER_SOCKET"); v != "" {
		c.Broker.Socket = v
	}
	if v := os.Getenv("HP_BROKER_TOKEN"); v != "" {
		c.Broker.Token = v
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
