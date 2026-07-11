package php_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/errx"
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
		SiteID: 1, User: "hps1", Home: "/srv/heropanel/sites/1", DocumentRoot: "/srv/heropanel/sites/1/public",
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if rec.PHPVersion != php.DefaultVersion || rec.SocketPath != "/run/heropanel/fpm/hps1.sock" {
		t.Fatalf("unexpected pool: %+v", rec)
	}
	// Broker received a rendered pool config with confinement.
	if len(gw.calls) != 1 {
		t.Fatalf("expected 1 broker call, got %d", len(gw.calls))
	}
	cfg, _ := gw.calls[0]["config"].(string)
	if !strings.Contains(cfg, "open_basedir") || !strings.Contains(cfg, "listen = /run/heropanel/fpm/hps1.sock") {
		t.Fatalf("pool config missing confinement/socket:\n%s", cfg)
	}
	// Round-trips from the store.
	got, err := svc.GetBySiteID(ctx, 1)
	if err != nil || got.PHPVersion != php.DefaultVersion {
		t.Fatalf("get = %+v err=%v", got, err)
	}
}

func TestEnsurePoolRejectsUnsupportedVersion(t *testing.T) {
	svc, _ := newPHP(t)
	_, err := svc.EnsurePool(context.Background(), php.PoolRequest{
		SiteID: 1, User: "hps1", Home: "/h", DocumentRoot: "/h/public", Version: "5.6",
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestEnsurePoolUpsertUpdates(t *testing.T) {
	svc, _ := newPHP(t)
	ctx := context.Background()
	if _, err := svc.EnsurePool(ctx, php.PoolRequest{SiteID: 1, User: "hps1", Home: "/h", DocumentRoot: "/h/public", Version: "8.1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsurePool(ctx, php.PoolRequest{SiteID: 1, User: "hps1", Home: "/h", DocumentRoot: "/h/public", Version: "8.3"}); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetBySiteID(ctx, 1)
	if got.PHPVersion != "8.3" {
		t.Fatalf("version = %q, want 8.3 (upsert)", got.PHPVersion)
	}
}
