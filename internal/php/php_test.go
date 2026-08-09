package php_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/php"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

type recordingGateway struct{ calls []map[string]any }

func (g *recordingGateway) Invoke(_ context.Context, _ string, input any) (map[string]any, error) {
	if m, ok := input.(map[string]any); ok {
		g.calls = append(g.calls, m)
	}
	return map[string]any{"ok": true}, nil
}
func (g *recordingGateway) Health(context.Context) error { return nil }

func newPHP(t *testing.T) (*php.Service, *recordingGateway) {
	t.Helper()
	dsn := t.TempDir() + "/php.db"
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	gw := &recordingGateway{}
	return php.NewService(repository.NewPHPPoolStore(db), gw), gw
}

func TestEnsurePoolWritesAndPersists(t *testing.T) {
	svc, gw := newPHP(t)
	ctx := context.Background()

	rec, err := svc.EnsurePool(ctx, php.PoolRequest{
		SiteID: 1, User: "nps1", Home: "/srv/nexpanel/sites/1", DocumentRoot: "/srv/nexpanel/sites/1/public",
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if rec.PHPVersion != php.DefaultVersion || rec.SocketPath != "/run/nexpanel/fpm/nps1.sock" {
		t.Fatalf("unexpected pool: %+v", rec)
	}
	// Broker received a rendered pool config with confinement.
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 broker call, got %d", len(gw.calls))
	}
	cfg, _ := gw.calls[0]["config"].(string)
	if !strings.Contains(cfg, "open_basedir") || !strings.Contains(cfg, "listen = /run/nexpanel/fpm/nps1.sock") {
		t.Fatalf("pool config missing confinement/socket:\n%s", cfg)
	}
	// Per-site isolation: the pool is confined to its own tree with NO shared /tmp
	// (that would leak temp files across sites), all temp paths redirected in-site,
	// and only real PHP executable.
	home := "/srv/nexpanel/sites/1"
	for _, must := range []string{
		"open_basedir] = " + home + "/\n",
		"sys_temp_dir] = " + home + "/tmp",
		"upload_tmp_dir] = " + home + "/tmp",
		"session.save_path] = " + home + "/sessions",
		"env[TMPDIR] = " + home + "/tmp",
		"security.limit_extensions = .php",
		"cgi.fix_pathinfo] = 0",
	} {
		if !strings.Contains(cfg, must) {
			t.Errorf("pool config missing isolation directive %q:\n%s", must, cfg)
		}
	}
	// The shared system /tmp must never appear in open_basedir.
	if strings.Contains(cfg, "open_basedir] = "+home+"/:/tmp") {
		t.Errorf("pool still grants access to the shared /tmp (cross-site leak):\n%s", cfg)
	}
	// Round-trips from the store.
	got, err := svc.GetBySiteID(ctx, 1)
	if err != nil || got.PHPVersion != php.DefaultVersion {
		t.Fatalf("get = %+v err=%v", got, err)
	}
}

// The default pool disables the dangerous function baseline, and the directive
// is a php_admin_value (so the ini editor can never restore it). Switching a site
// to the "off" tier removes the directive, and the choice round-trips from store.
func TestFuncPolicyRendersAndPersists(t *testing.T) {
	svc, gw := newPHP(t)
	ctx := context.Background()
	req := php.PoolRequest{SiteID: 1, User: "nps1", Home: "/srv/nexpanel/sites/1", DocumentRoot: "/srv/nexpanel/sites/1/public"}

	// Default (strict): disable_functions present as admin value with exec family.
	if _, err := svc.EnsurePool(ctx, req); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	cfg, _ := gw.calls[0]["config"].(string)
	if !strings.Contains(cfg, "php_admin_value[disable_functions] = ") || !strings.Contains(cfg, "exec") {
		t.Fatalf("default pool did not disable the dangerous baseline:\n%s", cfg)
	}
	got, err := svc.GetBySiteID(ctx, 1)
	if err != nil || got.FuncPolicy != php.FuncPolicyStrict {
		t.Fatalf("stored policy = %q err=%v, want strict", got.FuncPolicy, err)
	}

	// Relax to off: the directive disappears entirely.
	s := php.SettingsOf(got)
	s.FuncPolicy = php.FuncPolicyOff
	if _, err := svc.ApplySettings(ctx, req, s); err != nil {
		t.Fatalf("apply off: %v", err)
	}
	cfg2, _ := gw.calls[len(gw.calls)-1]["config"].(string)
	if strings.Contains(cfg2, "disable_functions") {
		t.Fatalf("off tier should emit no disable_functions:\n%s", cfg2)
	}
	got2, _ := svc.GetBySiteID(ctx, 1)
	if got2.FuncPolicy != php.FuncPolicyOff {
		t.Fatalf("stored policy = %q, want off", got2.FuncPolicy)
	}
}

func TestEnsurePoolRejectsUnsupportedVersion(t *testing.T) {
	svc, _ := newPHP(t)
	_, err := svc.EnsurePool(context.Background(), php.PoolRequest{
		SiteID: 1, User: "nps1", Home: "/h", DocumentRoot: "/h/public", Version: "5.6",
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestEnsurePoolUpsertUpdates(t *testing.T) {
	svc, _ := newPHP(t)
	ctx := context.Background()
	if _, err := svc.EnsurePool(ctx, php.PoolRequest{SiteID: 1, User: "nps1", Home: "/h", DocumentRoot: "/h/public", Version: "8.1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsurePool(ctx, php.PoolRequest{SiteID: 1, User: "nps1", Home: "/h", DocumentRoot: "/h/public", Version: "8.3"}); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetBySiteID(ctx, 1)
	if got.PHPVersion != "8.3" {
		t.Fatalf("version = %q, want 8.3 (upsert)", got.PHPVersion)
	}
}
