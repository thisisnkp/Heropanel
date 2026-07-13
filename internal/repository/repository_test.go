package repository_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// newTestDB opens a fresh file-backed SQLite database and migrates it.
func newTestDB(t *testing.T) *repository.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	applied, err := repository.Migrate(context.Background(), db)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if applied != 5 {
		t.Fatalf("applied %d migrations, want 5", applied)
	}
	return db
}

func TestMigrateCreatesSchemaAndIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// The expected Phase-0 tables must exist.
	want := []string{"users", "roles", "permissions", "role_permissions",
		"user_roles", "sessions", "api_keys", "audit_log", "settings", "jobs",
		"schema_migrations"}
	for _, table := range want {
		var name string
		err := db.GetContext(ctx, &name,
			`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table)
		if err != nil {
			t.Fatalf("expected table %q to exist: %v", table, err)
		}
	}

	// Running Migrate again applies nothing.
	applied, err := repository.Migrate(ctx, db)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if applied != 0 {
		t.Fatalf("second migrate applied %d, want 0", applied)
	}
}

func TestUserCreateAndFetch(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := repository.NewUserRepository(db)

	if n, err := repo.Count(ctx); err != nil || n != 0 {
		t.Fatalf("initial count = %d, err=%v; want 0", n, err)
	}

	u := &repository.User{
		Email:        "ada@example.com",
		Username:     "ada",
		DisplayName:  "Ada Lovelace",
		PasswordHash: sql.NullString{String: "argon2id$...", Valid: true},
	}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == 0 || len(u.UID) != 26 {
		t.Fatalf("expected populated ID and 26-char UID, got id=%d uid=%q", u.ID, u.UID)
	}

	got, err := repo.GetByEmail(ctx, "ada@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if got.Username != "ada" || got.UID != u.UID {
		t.Fatalf("fetched user mismatch: %+v", got)
	}

	byUID, err := repo.GetByUID(ctx, u.UID)
	if err != nil || byUID.Email != "ada@example.com" {
		t.Fatalf("get by uid failed: %+v err=%v", byUID, err)
	}

	if n, err := repo.Count(ctx); err != nil || n != 1 {
		t.Fatalf("count after insert = %d, err=%v; want 1", n, err)
	}
}

func TestGetByEmailNotFound(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewUserRepository(db)

	_, err := repo.GetByEmail(context.Background(), "nobody@example.com")
	if !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestUniqueEmailConflict(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	repo := repository.NewUserRepository(db)

	u1 := &repository.User{Email: "dup@example.com", Username: "u1"}
	if err := repo.Create(ctx, u1); err != nil {
		t.Fatalf("first create: %v", err)
	}
	u2 := &repository.User{Email: "dup@example.com", Username: "u2"}
	if err := repo.Create(ctx, u2); err == nil {
		t.Fatal("expected a conflict on duplicate email")
	}
}
