package auth_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/repository"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/pwhash"
)

// cheap keeps Argon2id fast in tests.
var cheap = pwhash.Params{Memory: 8 * 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func newDB(t *testing.T) *repository.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "auth.db")
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newService(t *testing.T, db *repository.DB, cfg auth.Config) *auth.Service {
	t.Helper()
	rbac := repository.NewRBACRepository(db)
	if err := auth.SeedRBAC(context.Background(), rbac); err != nil {
		t.Fatalf("seed rbac: %v", err)
	}
	l1 := pcache.NewLocal(pcache.LocalConfig{})
	t.Cleanup(func() { _ = l1.Close() })
	return auth.NewService(
		repository.NewUserRepository(db),
		repository.NewSessionRepository(db),
		rbac,
		pcache.NewTiered(l1, nil, pcache.TieredConfig{}),
		cfg,
	)
}

// seedUser inserts a ready-to-login user with a cheaply-hashed password.
func seedUser(t *testing.T, db *repository.DB, email, username, password, role string) {
	t.Helper()
	hash, err := pwhash.HashWith(password, cheap)
	if err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	u := &repository.User{
		Email:        email,
		Username:     username,
		DisplayName:  username,
		PasswordHash: sql.NullString{String: hash, Valid: true},
		Status:       "active",
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if role != "" {
		if err := repository.NewRBACRepository(db).AssignRole(context.Background(), u.ID, role); err != nil {
			t.Fatalf("assign role: %v", err)
		}
	}
}

func TestBootstrapCreatesSuperAdmin(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	ctx := context.Background()

	p, err := svc.Bootstrap(ctx, "admin@example.com", "admin", "supersecret1")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !p.Can("site.write") || !p.Can("anything") {
		t.Fatalf("admin should be superuser, perms=%v", p.Permissions)
	}

	// A second bootstrap must be refused.
	if _, err := svc.Bootstrap(ctx, "other@example.com", "other", "supersecret1"); !errx.IsKind(err, errx.KindConflict) {
		t.Fatalf("second bootstrap should conflict, got %v", err)
	}
}

func TestLoginAuthenticateLogout(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	ctx := context.Background()
	seedUser(t, db, "user@example.com", "user", "password123", "admin")

	token, p, err := svc.Login(ctx, "user@example.com", "password123", "1.2.3.4", "test-agent")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token == "" || p.Email != "user@example.com" {
		t.Fatalf("bad login result: token=%q p=%+v", token, p)
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.UserID != p.UserID || !got.Can("site.write") {
		t.Fatalf("authenticated principal mismatch: %+v", got)
	}

	if err := svc.Logout(ctx, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token); !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("authenticate after logout should be unauthorized, got %v", err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	seedUser(t, db, "user@example.com", "user", "password123", "client")

	_, _, err := svc.Login(context.Background(), "user@example.com", "WRONG", "1.2.3.4", "ua")
	if !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("want unauthorized, got %v", err)
	}
}

func TestLoginUnknownUser(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())

	_, _, err := svc.Login(context.Background(), "ghost@example.com", "whatever1", "1.2.3.4", "ua")
	if !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("want unauthorized (no enumeration), got %v", err)
	}
}

func TestLockoutAfterThreshold(t *testing.T) {
	db := newDB(t)
	cfg := auth.Config{SessionTTL: time.Hour, LockThreshold: 3, LockDuration: 15 * time.Minute, PrincipalCacheTTL: time.Second}
	svc := newService(t, db, cfg)
	seedUser(t, db, "user@example.com", "user", "password123", "client")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, _, err := svc.Login(ctx, "user@example.com", "WRONG", "1.2.3.4", "ua"); !errx.IsKind(err, errx.KindUnauthorized) {
			t.Fatalf("attempt %d: want unauthorized, got %v", i, err)
		}
	}
	// Now locked: even the correct password is refused with a lock error.
	if _, _, err := svc.Login(ctx, "user@example.com", "password123", "1.2.3.4", "ua"); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("after threshold, want forbidden (locked), got %v", err)
	}
}

func TestNonAdminHasScopedPermissions(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	seedUser(t, db, "client@example.com", "client", "password123", "client")

	_, p, err := svc.Login(context.Background(), "client@example.com", "password123", "1.2.3.4", "ua")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if p.Can("user.read") {
		t.Fatal("a plain client must not hold user.read")
	}
}
