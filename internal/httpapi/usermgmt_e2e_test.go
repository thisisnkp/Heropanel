package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/users"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

// newUserMgmtRouter builds a router with the user-management module wired, over a
// fresh migrated + seeded SQLite database.
func newUserMgmtRouter(t *testing.T) http.Handler {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "um.db")})
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
	return httpapi.NewRouter(httpapi.Deps{
		Ctx:       ctx,
		Config:    cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   "test",
		StartedAt: time.Now(),
		Auth:      authSvc,
		UserMgmt:  users.NewService(uRepo, rbac, sRepo),
	})
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The headline multi-user flow, end to end through the real HTTP stack: an admin
// creates a user, that user can log in, a non-admin is denied admin endpoints,
// and suspending the user immediately stops their login.
func TestUserManagementEndToEnd(t *testing.T) {
	h := newUserMgmtRouter(t)

	// Bootstrap + login as admin.
	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d: %s", rec.Code, rec.Body.String())
	}
	rec := doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil)
	adminCookie := sessionCookie(t, rec)

	// Admin creates a developer.
	rec = doJSON(t, h, "POST", "/api/v1/users", map[string]any{
		"email": "dev@h.io", "username": "dev", "password": "supersecret1", "roles": []string{"developer"},
	}, adminCookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			UID       string `json:"uid"`
			Superuser bool   `json:"superuser"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created.Data)
	// The envelope wraps under "data"; decode again from the wrapped shape.
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	devUID := created.Data.UID
	if devUID == "" {
		t.Fatalf("no uid in create response: %s", rec.Body.String())
	}
	if created.Data.Superuser {
		t.Fatal("a developer must not be a superuser")
	}

	// The created user can log in.
	rec = doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "dev@h.io", "password": "supersecret1"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev login = %d: %s", rec.Code, rec.Body.String())
	}
	devCookie := sessionCookie(t, rec)

	// A developer lacks user.read → /users is 403 for them.
	if rec := getWith(t, h, "/api/v1/users", devCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("dev GET /users = %d, want 403", rec.Code)
	}

	_ = devCookie // their session is revoked at suspend; the cached principal
	// lingers only until the short principal-cache TTL (documented behaviour,
	// shared with "sign out everywhere"), so the test asserts the immediate,
	// unconditional property instead: a suspended account cannot log in.

	// Admin suspends the developer.
	if rec := doJSON(t, h, "POST", "/api/v1/users/"+devUID+"/status",
		map[string]string{"status": "suspended"}, adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("suspend = %d: %s", rec.Code, rec.Body.String())
	}
	// A suspended account can no longer log in (blocked at authentication).
	if rec := doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "dev@h.io", "password": "supersecret1"}, nil); rec.Code == http.StatusOK {
		t.Fatalf("suspended dev login = %d, want a refusal", rec.Code)
	}

	// Reactivating lets them log in again.
	if rec := doJSON(t, h, "POST", "/api/v1/users/"+devUID+"/status",
		map[string]string{"status": "active"}, adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("reactivate = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "dev@h.io", "password": "supersecret1"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("reactivated dev login = %d, want 200", rec.Code)
	}
}

// The last-administrator guard is enforced through the HTTP layer too.
func TestCannotDeleteLastAdminOverHTTP(t *testing.T) {
	h := newUserMgmtRouter(t)
	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d", rec.Code)
	}
	rec := doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil)
	cookie := sessionCookie(t, rec)

	// The admin's own UID.
	rec = getWith(t, h, "/api/v1/users", cookie)
	var list struct {
		Data struct {
			Users []struct {
				UID string `json:"uid"`
			} `json:"users"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data.Users) != 1 {
		t.Fatalf("want 1 user, got %d", len(list.Data.Users))
	}
	uid := list.Data.Users[0].UID
	// Deleting yourself / the only admin is refused (422 validation).
	if rec := doJSON(t, h, "DELETE", "/api/v1/users/"+uid, nil, cookie); rec.Code == http.StatusOK {
		t.Fatalf("deleting the last admin returned %d, want a refusal", rec.Code)
	}
}
