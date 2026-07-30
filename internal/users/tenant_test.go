package users_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/users"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// A reseller manages an isolated tenant: accounts they create are parented under
// them, their listing shows only their subtree, and they cannot see or act on
// users in another tenant.
func TestResellerTenantIsolation(t *testing.T) {
	svc, uRepo, _, _ := newSvc(t)
	ctx := context.Background()
	mkAdmin(t, svc) // the platform admin (superuser)

	// Admin creates two independent resellers.
	resA, err := svc.Create(ctx, superActor(0), users.CreateUserInput{
		Email: "resa@h.io", Username: "resa", Password: "supersecret1", Roles: []string{"reseller"},
	})
	if err != nil {
		t.Fatalf("create resA: %v", err)
	}
	resB, err := svc.Create(ctx, superActor(0), users.CreateUserInput{
		Email: "resb@h.io", Username: "resb", Password: "supersecret1", Roles: []string{"reseller"},
	})
	if err != nil {
		t.Fatalf("create resB: %v", err)
	}
	rowA, _ := uRepo.GetByUID(ctx, resA.UID)
	rowB, _ := uRepo.GetByUID(ctx, resB.UID)
	actorA := users.Actor{UserID: rowA.ID, Permissions: []string{"user.read", "user.write"}}

	// Reseller A creates a client — it must be parented into A's tenant.
	client, err := svc.Create(ctx, actorA, users.CreateUserInput{
		Email: "client@h.io", Username: "client", Password: "supersecret1",
	})
	if err != nil {
		t.Fatalf("reseller create client: %v", err)
	}
	clientRow, _ := uRepo.GetByUID(ctx, client.UID)
	if parent, _ := uRepo.ParentID(ctx, clientRow.ID); parent != rowA.ID {
		t.Fatalf("client parent = %d, want reseller A %d", parent, rowA.ID)
	}

	// A's listing shows exactly A and its client — never B, its client, or the admin.
	list, err := svc.ListScoped(ctx, actorA, 100, 0)
	if err != nil {
		t.Fatalf("list scoped: %v", err)
	}
	seen := map[string]bool{}
	for _, u := range list {
		seen[u.Email] = true
	}
	if !seen["resa@h.io"] || !seen["client@h.io"] {
		t.Fatalf("A's list is missing its own tenant: %v", seen)
	}
	if seen["resb@h.io"] || seen["admin@h.io"] {
		t.Fatalf("A's list leaks another tenant: %v", seen)
	}

	// A cannot see or act on B (a different tenant) — reported as not-found.
	if _, err := svc.Get(ctx, actorA, resB.UID); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("A getting B = %v, want not-found", err)
	}
	if _, err := svc.SetStatus(ctx, actorA, resB.UID, "suspended"); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("A suspending B = %v, want not-found", err)
	}
	if err := svc.Delete(ctx, actorA, resB.UID); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("A deleting B = %v, want not-found", err)
	}
	_ = rowB
}

// A reseller cannot escalate: neither creating an administrator nor promoting a
// client to a role whose permissions exceed the reseller's own grant.
func TestResellerCannotEscalate(t *testing.T) {
	svc, uRepo, _, _ := newSvc(t)
	ctx := context.Background()
	mkAdmin(t, svc)

	res, err := svc.Create(ctx, superActor(0), users.CreateUserInput{
		Email: "res@h.io", Username: "res", Password: "supersecret1", Roles: []string{"reseller"},
	})
	if err != nil {
		t.Fatalf("create reseller: %v", err)
	}
	row, _ := uRepo.GetByUID(ctx, res.UID)
	// The reseller holds site.* but not user administration or the wildcard.
	actor := users.Actor{UserID: row.ID, Permissions: []string{"site.read", "site.write"}}

	// Creating an administrator is refused — "admin" grants "*".
	if _, err := svc.Create(ctx, actor, users.CreateUserInput{
		Email: "sneaky@h.io", Username: "sneaky", Password: "supersecret1", Roles: []string{"admin"},
	}); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("reseller creating an admin = %v, want forbidden", err)
	}

	// Make a client, then try to promote it to a role holding a permission the
	// reseller lacks.
	if _, err := svc.CreateRole(ctx, users.CreateRoleInput{
		Slug: "dbadmin", Name: "DB Admin", Permissions: []string{"database.write"},
	}); err != nil {
		t.Fatalf("seed custom role: %v", err)
	}
	client, err := svc.Create(ctx, actor, users.CreateUserInput{
		Email: "cli@h.io", Username: "cli", Password: "supersecret1",
	})
	if err != nil {
		t.Fatalf("reseller create client: %v", err)
	}
	if _, err := svc.SetRoles(ctx, actor, client.UID, []string{"dbadmin"}); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("reseller granting database.write it lacks = %v, want forbidden", err)
	}
}
