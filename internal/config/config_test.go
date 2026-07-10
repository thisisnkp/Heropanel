package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/config"
)

func TestDefaults(t *testing.T) {
	c := config.Default()
	if c.Server.Port != 8443 {
		t.Fatalf("default port = %d, want 8443", c.Server.Port)
	}
	if c.Log.Level != "info" || c.Log.Format != "json" {
		t.Fatalf("unexpected log defaults: %+v", c.Log)
	}
	if c.Server.ReadTimeout.D() != 15*time.Second {
		t.Fatalf("read timeout = %v", c.Server.ReadTimeout.D())
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("HP_SERVER_PORT", "9000")
	t.Setenv("HP_LOG_LEVEL", "debug")
	t.Setenv("HP_REDIS_ADDR", "10.0.0.5:6379")

	c, err := config.Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Server.Port != 9000 {
		t.Fatalf("port = %d, want 9000 (env)", c.Server.Port)
	}
	if c.Log.Level != "debug" {
		t.Fatalf("level = %q, want debug (env)", c.Log.Level)
	}
	if c.Redis.Addr != "10.0.0.5:6379" {
		t.Fatalf("redis addr = %q", c.Redis.Addr)
	}
}

func TestFileThenEnvLayering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
server:
  port: 8080
  read_timeout: 5s
log:
  level: warn
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Env must win over the file.
	t.Setenv("HP_LOG_LEVEL", "error")

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Server.Port != 8080 {
		t.Fatalf("port = %d, want 8080 (file)", c.Server.Port)
	}
	if c.Server.ReadTimeout.D() != 5*time.Second {
		t.Fatalf("read_timeout = %v, want 5s (file)", c.Server.ReadTimeout.D())
	}
	if c.Log.Level != "error" {
		t.Fatalf("level = %q, want error (env overrides file)", c.Log.Level)
	}
	// Unspecified fields keep their defaults.
	if c.Server.Host != "0.0.0.0" {
		t.Fatalf("host = %q, want default 0.0.0.0", c.Server.Host)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	c := config.Default()
	c.Server.Port = 70000
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for out-of-range port")
	}

	c = config.Default()
	c.Server.TLS.Enabled = true // missing cert/key
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for TLS without cert/key")
	}

	c = config.Default()
	c.Database.Driver = "oracle"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for unsupported db driver")
	}
}
