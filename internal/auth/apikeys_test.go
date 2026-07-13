package auth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func newServiceWithKeys(t *testing.T, db *repository.DB) *auth.Service {
	t.Helper()
	svc := newService(t, db, auth.DefaultConfig())
	return svc.WithAPIKeys(repository.NewAPIKeyRepository(db))
}

func TestAPIKeyCreateAuthenticateRevoke(t *testing.T) {
	db := newDB(t)
	svc := newServiceWithKeys(t, db)
	ctx := context.Background()
	seedUser(t, db, "owner@example.com", "owner", "password123", "admin")

	// user id 1 is the seeded owner.
	key, view, err := svc.CreateAPIKey(ctx, 1, "ci-token", []string{"site.read"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(key, "hp_") || view.Prefix != key[:14] {
		t.Fatalf("bad key/view: key=%q view=%+v", key, view)
	}

	// Authenticate with the plaintext key -> scoped principal.
	p, err := svc.AuthenticateAPIKey(ctx, key)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !p.Can("site.read") || p.Can("site.write") {
		t.Fatalf("scopes not applied: %v", p.Permissions)
	}

	// A wrong key is rejected.
	if _, err := svc.AuthenticateAPIKey(ctx, key+"tampered"); !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("tampered key should be unauthorized, got %v", err)
	}

	// Listing shows the key (without the secret).
	keys, _ := svc.ListAPIKeys(ctx, 1)
	if len(keys) != 1 || keys[0].UID != view.UID {
		t.Fatalf("list = %+v", keys)
	}

	// Revoke -> authentication fails.
	if err := svc.RevokeAPIKey(ctx, 1, view.UID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.AuthenticateAPIKey(ctx, key); !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("revoked key should be unauthorized, got %v", err)
	}
}
