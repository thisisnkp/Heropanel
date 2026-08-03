package repository_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

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
	if applied != 41 {
		t.Fatalf("applied %d migrations, want 41", applied)
	}
	return db
}

func TestMigrateCreatesSchemaAndIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// The expected Phase-0 tables must exist.
	want := []string{"users", "roles", "permissions", "role_permissions",
		"user_roles", "sessions", "api_keys", "audit_log", "settings", "jobs",
		"schema_migrations", "git_sources", "git_deployments", "app_runtimes",
		"dns_zones", "dns_records", "db_sso_sessions", "site_limits"}
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

// Session management: a user sees only their own active sessions, can revoke
// one by UID, "sign out everywhere else" keeps only the current one, and no
// user can revoke another user's session.
func TestSessionManagement(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)
	now := time.Now()

	u1 := &repository.User{Email: "u1@h.io", Username: "u1", DisplayName: "u1", Status: "active"}
	u2 := &repository.User{Email: "u2@h.io", Username: "u2", DisplayName: "u2", Status: "active"}
	if err := users.Create(ctx, u1); err != nil {
		t.Fatal(err)
	}
	if err := users.Create(ctx, u2); err != nil {
		t.Fatal(err)
	}

	mk := func(uid string, userID int64, hash string) {
		if err := sessions.Create(ctx, &repository.Session{
			UID: uid, UserID: userID, TokenHash: hash, IP: "203.0.113.1",
			UserAgent: "test", ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("s1a", u1.ID, "h1a") // u1 current
	mk("s1b", u1.ID, "h1b")
	mk("s1c", u1.ID, "h1c")
	mk("s2a", u2.ID, "h2a") // another user's

	// List returns only u1's three sessions.
	got, err := sessions.ListActiveByUser(ctx, u1.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("u1 has %d sessions, want 3", len(got))
	}

	// u1 cannot revoke u2's session (scoped by user_id -> not found).
	if err := sessions.RevokeByUIDForUser(ctx, u1.ID, "s2a", now); err == nil {
		t.Error("u1 revoked another user's session")
	}

	// u1 revokes one of their own.
	if err := sessions.RevokeByUIDForUser(ctx, u1.ID, "s1b", now); err != nil {
		t.Fatalf("revoke own: %v", err)
	}
	got, _ = sessions.ListActiveByUser(ctx, u1.ID, now)
	if len(got) != 2 {
		t.Fatalf("after revoke, u1 has %d, want 2", len(got))
	}

	// Sign out everywhere else keeps only the current (h1a).
	n, err := sessions.RevokeAllForUserExcept(ctx, u1.ID, "h1a", now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 { // only s1c remained active besides the current
		t.Fatalf("revoke-others revoked %d, want 1", n)
	}
	got, _ = sessions.ListActiveByUser(ctx, u1.ID, now)
	if len(got) != 1 || got[0].UID != "s1a" {
		t.Fatalf("only the current session should remain: %+v", got)
	}
	// u2 is untouched.
	if g2, _ := sessions.ListActiveByUser(ctx, u2.ID, now); len(g2) != 1 {
		t.Errorf("u2's session was affected: %d", len(g2))
	}
}
