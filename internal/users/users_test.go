package users_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/users"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// newSvc opens a migrated SQLite DB with the seeded RBAC catalog and returns the
// management service plus the raw repos for arranging state.
func newSvc(t *testing.T) (*users.Service, *repository.UserRepository, *repository.RBACRepository, *repository.SessionRepository) {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "u.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rbac := repository.NewRBACRepository(db)
	if err := auth.SeedRBAC(context.Background(), rbac); err != nil {
		t.Fatalf("seed: %v", err)
	}
	uRepo := repository.NewUserRepository(db)
	sRepo := repository.NewSessionRepository(db)
	return users.NewService(uRepo, rbac, sRepo), uRepo, rbac, sRepo
}

// superActor stands in for an administrator (superuser) performing a management
// action: it bypasses tenant scoping and the role-escalation guard, exactly as a
// real "*" principal does.
func superActor(id int64) users.Actor { return users.Actor{UserID: id, Superuser: true} }

// mkAdmin creates a user and assigns the admin role, returning the UID + id.
func mkAdmin(t *testing.T, svc *users.Service) *users.UserView {
	t.Helper()
	v, err := svc.Create(context.Background(), superActor(0), users.CreateUserInput{
		Email: "admin@h.io", Username: "admin", Password: "supersecret1", Roles: []string{"admin"},
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if !v.Superuser {
		t.Fatal("admin should be a superuser")
	}
	return v
}

func TestCreateAndListUsers(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	ctx := context.Background()
	mkAdmin(t, svc)

	dev, err := svc.Create(ctx, superActor(0), users.CreateUserInput{
		Email: "dev@h.io", Username: "dev", Password: "supersecret1", Roles: []string{"developer"},
	})
	if err != nil {
		t.Fatalf("create dev: %v", err)
	}
	if dev.Superuser {
		t.Error("developer should not be a superuser")
	}
	if len(dev.Roles) != 1 || dev.Roles[0] != "developer" {
		t.Errorf("roles = %v", dev.Roles)
	}
	list, err := svc.ListScoped(ctx, superActor(0), 50, 0)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %d (err %v), want 2", len(list), err)
	}
}

func TestCreateValidation(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	ctx := context.Background()
	cases := []users.CreateUserInput{
		{Email: "bad", Username: "u1", Password: "supersecret1"},
		{Email: "a@b.co", Username: "x", Password: "supersecret1"}, // username too short
		{Email: "a@b.co", Username: "user1", Password: "short"},
		{Email: "a@b.co", Username: "user1", Password: "supersecret1", Roles: []string{"nope"}},
	}
	for i, in := range cases {
		if _, err := svc.Create(ctx, superActor(0), in); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

// The last active administrator cannot be suspended, demoted, or deleted.
func TestLastSuperuserGuard(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	ctx := context.Background()
	admin := mkAdmin(t, svc)

	// Suspend the only admin → refused.
	if _, err := svc.SetStatus(ctx, superActor(999), admin.UID, "suspended"); err == nil {
		t.Fatal("suspending the last admin should be refused")
	}
	// Demote the only admin → refused.
	if _, err := svc.SetRoles(ctx, superActor(999), admin.UID, []string{"developer"}); err == nil {
		t.Fatal("demoting the last admin should be refused")
	}
	// Delete the only admin → refused.
	if err := svc.Delete(ctx, superActor(999), admin.UID); err == nil {
		t.Fatal("deleting the last admin should be refused")
	}

	// With a SECOND admin, demoting the first is allowed.
	second, err := svc.Create(ctx, superActor(0), users.CreateUserInput{
		Email: "a2@h.io", Username: "admin2", Password: "supersecret1", Roles: []string{"admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRoles(ctx, superActor(999), admin.UID, []string{"developer"}); err != nil {
		t.Fatalf("demote with a spare admin should succeed: %v", err)
	}
	// Now `second` is the last admin — deleting it is refused again.
	if err := svc.Delete(ctx, superActor(999), second.UID); err == nil {
		t.Fatal("deleting the now-last admin should be refused")
	}
}

func TestCannotActOnSelf(t *testing.T) {
	svc, uRepo, _, _ := newSvc(t)
	ctx := context.Background()
	admin := mkAdmin(t, svc)
	u, _ := uRepo.GetByUID(ctx, admin.UID)

	if _, err := svc.SetStatus(ctx, superActor(u.ID), admin.UID, "suspended"); err == nil {
		t.Error("suspending yourself should be refused")
	}
	if err := svc.Delete(ctx, superActor(u.ID), admin.UID); err == nil {
		t.Error("deleting yourself should be refused")
	}
}

// Suspending a user ends their sessions.
func TestSuspendRevokesSessions(t *testing.T) {
	svc, uRepo, _, sRepo := newSvc(t)
	ctx := context.Background()
	mkAdmin(t, svc)
	dev, _ := svc.Create(ctx, superActor(0), users.CreateUserInput{
		Email: "dev@h.io", Username: "dev", Password: "supersecret1", Roles: []string{"developer"},
	})
	u, _ := uRepo.GetByUID(ctx, dev.UID)
	now := time.Now()
	if err := sRepo.Create(ctx, &repository.Session{
		UID: "s1", UserID: u.ID, TokenHash: "h1", IP: "203.0.113.1", UserAgent: "t", ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStatus(ctx, superActor(999), dev.UID, "suspended"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	active, _ := sRepo.ListActiveByUser(ctx, u.ID, now)
	if len(active) != 0 {
		t.Fatalf("suspended user still has %d active sessions", len(active))
	}
}

func TestDeleteFreesEmail(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	ctx := context.Background()
	mkAdmin(t, svc)
	dev, _ := svc.Create(ctx, superActor(0), users.CreateUserInput{
		Email: "dev@h.io", Username: "dev", Password: "supersecret1",
	})
	if err := svc.Delete(ctx, superActor(999), dev.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// The email is freed: a new account can reuse it.
	if _, err := svc.Create(ctx, superActor(0), users.CreateUserInput{
		Email: "dev@h.io", Username: "dev", Password: "supersecret1",
	}); err != nil {
		t.Fatalf("reuse freed email: %v", err)
	}
}

// ── roles ────────────────────────────────────────────────────────────────────

func TestCustomRoleLifecycle(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	ctx := context.Background()

	role, err := svc.CreateRole(ctx, users.CreateRoleInput{
		Slug: "support", Name: "Support", Permissions: []string{"site.read", "audit.read"},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if role.System || len(role.Permissions) != 2 {
		t.Fatalf("bad custom role: %+v", role)
	}
	// Update permissions.
	perms := []string{"site.read"}
	updated, err := svc.UpdateRole(ctx, "support", users.UpdateRoleInput{Name: "Support Team", Permissions: &perms})
	if err != nil {
		t.Fatalf("update role: %v", err)
	}
	if updated.Name != "Support Team" || len(updated.Permissions) != 1 {
		t.Fatalf("update wrong: %+v", updated)
	}
	// Delete it.
	if err := svc.DeleteRole(ctx, "support"); err != nil {
		t.Fatalf("delete role: %v", err)
	}
}

func TestSystemRoleAndWildcardGuards(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	ctx := context.Background()

	// A custom role cannot hold the wildcard.
	if _, err := svc.CreateRole(ctx, users.CreateRoleInput{
		Slug: "godmode", Name: "God", Permissions: []string{"*"},
	}); err == nil {
		t.Error("a custom role with '*' should be refused")
	}
	// A system role's permissions are locked.
	perms := []string{"site.read"}
	if _, err := svc.UpdateRole(ctx, "admin", users.UpdateRoleInput{Permissions: &perms}); err == nil {
		t.Error("editing a system role's permissions should be refused")
	}
	// A system role cannot be deleted.
	if err := svc.DeleteRole(ctx, "developer"); err == nil {
		t.Error("deleting a system role should be refused")
	}
	// An unknown permission is rejected.
	if _, err := svc.CreateRole(ctx, users.CreateRoleInput{
		Slug: "x", Name: "X", Permissions: []string{"not.a.perm"},
	}); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("unknown permission should be a validation error, got %v", err)
	}
}
