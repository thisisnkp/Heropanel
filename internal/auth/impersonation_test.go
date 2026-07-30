package auth_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// repoUser returns a seeded user record by email.
func repoUser(t *testing.T, db *repository.DB, email string) *repository.User {
	t.Helper()
	u, err := repository.NewUserRepository(db).GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("lookup %s: %v", email, err)
	}
	return u
}

// uidFor returns a seeded user's UID by email.
func uidFor(t *testing.T, db *repository.DB, email string) string {
	t.Helper()
	return repoUser(t, db, email).UID
}

func TestStartImpersonationActsAsTargetAndIsAttributed(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	ctx := context.Background()

	seedUser(t, db, "admin@example.com", "admin", "adminpass1", "admin")
	seedUser(t, db, "dev@example.com", "dev", "devpass123", "developer")
	admin := repoUser(t, db, "admin@example.com")
	devUID := uidFor(t, db, "dev@example.com")

	imp, err := svc.StartImpersonation(ctx, admin.ID, devUID, "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("start impersonation: %v", err)
	}
	if imp.Target.UserUID != devUID {
		t.Fatalf("target uid = %s, want %s", imp.Target.UserUID, devUID)
	}

	// Resolving the minted session must yield the developer's identity, carrying
	// the admin as impersonator — and crucially not the admin's permissions.
	p, err := svc.Authenticate(ctx, imp.SessionToken)
	if err != nil {
		t.Fatalf("authenticate impersonation token: %v", err)
	}
	if p.UserUID != devUID {
		t.Fatalf("principal uid = %s, want the developer %s", p.UserUID, devUID)
	}
	if !p.Impersonated() {
		t.Fatal("principal should report Impersonated()")
	}
	if p.ImpersonatorUserID != admin.ID || p.ImpersonatorEmail != "admin@example.com" {
		t.Fatalf("impersonator = (%d,%s), want (%d,admin@example.com)", p.ImpersonatorUserID, p.ImpersonatorEmail, admin.ID)
	}
	if p.Can("*") {
		t.Fatal("impersonated session must not inherit the admin's superuser permission")
	}
}

func TestStopImpersonationRestoresAdmin(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	ctx := context.Background()

	seedUser(t, db, "admin@example.com", "admin", "adminpass1", "admin")
	seedUser(t, db, "dev@example.com", "dev", "devpass123", "developer")
	admin := repoUser(t, db, "admin@example.com")

	imp, err := svc.StartImpersonation(ctx, admin.ID, uidFor(t, db, "dev@example.com"), "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	res, err := svc.StopImpersonation(ctx, imp.SessionToken, "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if res.Principal.UserID != admin.ID || res.Principal.Impersonated() {
		t.Fatalf("stop returned %+v, want the admin's own identity", res.Principal)
	}
	// The impersonation session is dead; only the fresh admin session works.
	if _, err := svc.Authenticate(ctx, imp.SessionToken); !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("old impersonation token still valid: %v", err)
	}
	p, err := svc.Authenticate(ctx, res.SessionToken)
	if err != nil {
		t.Fatalf("authenticate restored session: %v", err)
	}
	if p.UserID != admin.ID || p.Impersonated() {
		t.Fatalf("restored principal = %+v, want the plain admin", p)
	}
}

func TestStopWhenNotImpersonating(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	ctx := context.Background()

	seedUser(t, db, "admin@example.com", "admin", "adminpass1", "admin")
	lr, err := svc.Login(ctx, "admin@example.com", "adminpass1", "10.0.0.1", "ua")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := svc.StopImpersonation(ctx, lr.SessionToken, "10.0.0.1", "ua"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("stop on a non-impersonation session = %v, want validation error", err)
	}
}

func TestImpersonationGuards(t *testing.T) {
	db := newDB(t)
	svc := newService(t, db, auth.DefaultConfig())
	ctx := context.Background()

	seedUser(t, db, "admin@example.com", "admin", "adminpass1", "admin")
	seedUser(t, db, "admin2@example.com", "admin2", "adminpass2", "admin")
	seedUser(t, db, "dev@example.com", "dev", "devpass123", "developer")
	admin := repoUser(t, db, "admin@example.com")

	// Cannot impersonate yourself.
	if _, err := svc.StartImpersonation(ctx, admin.ID, uidFor(t, db, "admin@example.com"), "ip", "ua"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("self impersonation = %v, want validation error", err)
	}
	// Cannot impersonate another administrator (would grant the wildcard).
	if _, err := svc.StartImpersonation(ctx, admin.ID, uidFor(t, db, "admin2@example.com"), "ip", "ua"); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("superuser impersonation = %v, want forbidden", err)
	}
	// Cannot impersonate a suspended user.
	users := repository.NewUserRepository(db)
	if err := users.SetStatus(ctx, uidFor(t, db, "dev@example.com"), "suspended"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := svc.StartImpersonation(ctx, admin.ID, uidFor(t, db, "dev@example.com"), "ip", "ua"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("inactive impersonation = %v, want validation error", err)
	}
}
