package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/users"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

// newImpersonationRouter wires Auth + UserMgmt + a real audit chain over one
// SQLite database, and hands back the db so a test can read the chain directly.
func newImpersonationRouter(t *testing.T) (http.Handler, *repository.DB) {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "imp.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	uRepo := repository.NewUserRepository(db)
	sRepo := repository.NewSessionRepository(db)
	rbac := repository.NewRBACRepository(db)
	if err := auth.SeedRBAC(context.Background(), rbac); err != nil {
		t.Fatalf("seed: %v", err)
	}
	l1 := pcache.NewLocal(pcache.LocalConfig{})
	t.Cleanup(func() { _ = l1.Close() })
	authSvc := auth.NewService(uRepo, sRepo, rbac, pcache.NewTiered(l1, nil, pcache.TieredConfig{}), auth.DefaultConfig())

	cfg := config.Default()
	cfg.Security.RateLimit.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := httpapi.NewRouter(httpapi.Deps{
		Ctx:       ctx,
		Config:    cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   "test",
		StartedAt: time.Now(),
		Auth:      authSvc,
		UserMgmt:  users.NewService(uRepo, rbac, sRepo),
		Audit:     audit.NewService(repository.NewAuditRepository(db)),
	})
	return h, db
}

// The headline impersonation flow, end to end: an admin becomes a developer,
// operates with the developer's (limited) permissions, and the mutation is
// attributed in the audit chain to the admin — not the developer — naming whom
// they acted as. Then the admin steps back to their own identity.
func TestImpersonationEndToEnd(t *testing.T) {
	h, db := newImpersonationRouter(t)
	ctx := context.Background()

	// Bootstrap + login as admin.
	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d: %s", rec.Code, rec.Body.String())
	}
	adminCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil))

	// Admin creates a developer and captures their UID.
	rec := doJSON(t, h, "POST", "/api/v1/users", map[string]any{
		"email": "dev@h.io", "username": "dev", "password": "supersecret1", "roles": []string{"developer"},
	}, adminCookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			UID string `json:"uid"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	devUID := created.Data.UID
	if devUID == "" {
		t.Fatalf("no dev uid: %s", rec.Body.String())
	}

	// Admin impersonates the developer; the response is the developer's principal
	// and the Set-Cookie swaps the session to the impersonation one.
	rec = doJSON(t, h, "POST", "/api/v1/users/"+devUID+"/impersonate", nil, adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("impersonate = %d: %s", rec.Code, rec.Body.String())
	}
	impCookie := sessionCookie(t, rec)

	// /me under the impersonation session: acting as the dev, but naming the admin
	// as the impersonator behind it.
	rec = getWith(t, h, "/api/v1/auth/me", impCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d: %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Data struct {
			UserUID           string `json:"user_uid"`
			ImpersonatorEmail string `json:"impersonator_email"`
			ImpersonatorUID   string `json:"impersonator_uid"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Data.UserUID != devUID {
		t.Fatalf("me.user_uid = %s, want the dev %s", me.Data.UserUID, devUID)
	}
	if me.Data.ImpersonatorEmail != "admin@h.io" {
		t.Fatalf("me.impersonator_email = %q, want admin@h.io", me.Data.ImpersonatorEmail)
	}

	// The impersonated session carries only the developer's rights: an admin-only
	// route is refused, proving impersonation is not a privilege escalation.
	if rec := getWith(t, h, "/api/v1/users", impCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("impersonated GET /users = %d, want 403", rec.Code)
	}

	// A mutation made while impersonating (here, stopping) must be attributed to
	// the admin in the audit chain, tagged with whom they acted as.
	rec = doJSON(t, h, "POST", "/api/v1/auth/impersonation/stop", nil, impCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop = %d: %s", rec.Code, rec.Body.String())
	}
	var stopped struct {
		Data struct {
			UserUID         string `json:"user_uid"`
			ImpersonatorUID string `json:"impersonator_uid"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &stopped)
	admin, err := repository.NewUserRepository(db).GetByEmail(ctx, "admin@h.io")
	if err != nil {
		t.Fatalf("lookup admin: %v", err)
	}
	if stopped.Data.UserUID != admin.UID || stopped.Data.ImpersonatorUID != "" {
		t.Fatalf("stop returned %+v, want the admin's own identity", stopped.Data)
	}

	// The audit row for the stop is filed under the admin, not the dev, and names
	// the impersonated user.
	entries, err := audit.NewService(repository.NewAuditRepository(db)).
		List(ctx, audit.Filter{Action: "POST /api/v1/auth/impersonation/stop", Limit: 10})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no audit entry for the impersonation stop")
	}
	e := entries[0]
	if e.ActorUserID != admin.ID {
		t.Fatalf("stop attributed to actor %d, want the admin %d", e.ActorUserID, admin.ID)
	}
	if e.ActorKind != audit.ActorUser {
		t.Fatalf("stop actor kind = %q, want user", e.ActorKind)
	}
	if !strings.Contains(e.Detail, devUID) {
		t.Fatalf("stop audit detail %q does not name the impersonated user %s", e.Detail, devUID)
	}
}

// Impersonating an administrator is refused over HTTP: it would hand the actor
// the wildcard permission.
func TestCannotImpersonateAdminOverHTTP(t *testing.T) {
	h, _ := newImpersonationRouter(t)
	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d", rec.Code)
	}
	adminCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil))

	// A second administrator.
	rec := doJSON(t, h, "POST", "/api/v1/users", map[string]any{
		"email": "admin2@h.io", "username": "admin2", "password": "supersecret1", "roles": []string{"admin"},
	}, adminCookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create admin2 = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			UID string `json:"uid"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	if rec := doJSON(t, h, "POST", "/api/v1/users/"+created.Data.UID+"/impersonate", nil, adminCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("impersonating an admin = %d, want 403", rec.Code)
	}
}
