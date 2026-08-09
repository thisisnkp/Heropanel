package repository_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/marketplace"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// TestModuleStore exercises the installed-module record lifecycle against a real
// SQLite database: insert, re-install (upsert), state change, list, delete, and
// the not-found paths.
func TestModuleStore(t *testing.T) {
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mod.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	store := repository.NewModuleStore(db)

	rec := marketplace.InstalledModule{
		Slug: "backups-pro", Name: "Backups Pro", Version: "2.1.0",
		Category: "backup", State: marketplace.StateInstalled, PublisherKey: "abc123def456",
	}
	if err := store.Upsert(ctx, rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.Get(ctx, "backups-pro")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Version != "2.1.0" || got.State != marketplace.StateInstalled || got.PublisherKey != "abc123def456" {
		t.Fatalf("record round-trip wrong: %+v", got)
	}
	if got.InstalledAt == "" || got.UpdatedAt == "" {
		t.Error("timestamps not populated")
	}

	// Re-install a newer version: the record updates in place, and the operator's
	// state is preserved (upsert does not reset it).
	if err := store.SetState(ctx, "backups-pro", marketplace.StateEnabled); err != nil {
		t.Fatalf("setstate: %v", err)
	}
	rec.Version = "2.2.0"
	if err := store.Upsert(ctx, rec); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _ = store.Get(ctx, "backups-pro")
	if got.Version != "2.2.0" {
		t.Errorf("upsert did not update version: %q", got.Version)
	}
	if got.State != marketplace.StateEnabled {
		t.Errorf("upsert clobbered state: %q", got.State)
	}

	list, err := store.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: len=%d err=%v", len(list), err)
	}

	if err := store.Delete(ctx, "backups-pro"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, "backups-pro"); errx.KindOf(err) != errx.KindNotFound {
		t.Errorf("get after delete: want not_found, got %v", errx.KindOf(err))
	}
	if err := store.SetState(ctx, "ghost", marketplace.StateEnabled); errx.KindOf(err) != errx.KindNotFound {
		t.Errorf("setstate on missing: want not_found, got %v", errx.KindOf(err))
	}
}
