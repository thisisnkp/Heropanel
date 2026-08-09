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

	"github.com/thisisnkp/nexpanel/internal/auth"
	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/httpapi"
	"github.com/thisisnkp/nexpanel/internal/repository"
	pcache "github.com/thisisnkp/nexpanel/pkg/cache"
)

// testUserDir adapts the user repository to httpapi.UserDirectory for the test.
type testUserDir struct{ repo *repository.UserRepository }

func (d testUserDir) List(ctx context.Context, limit, offset int) ([]httpapi.UserSummary, error) {
	rows, err := d.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]httpapi.UserSummary, len(rows))
	for i, u := range rows {
		out[i] = httpapi.UserSummary{UID: u.UID, Email: u.Email, Username: u.Username, DisplayName: u.DisplayName, Status: u.Status}
	}
	return out, nil
}

func newAuthRouter(t *testing.T) http.Handler {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "e2e.db")
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)
	rbac := repository.NewRBACRepository(db)
	if err := auth.SeedRBAC(context.Background(), rbac); err != nil {
		t.Fatalf("seed: %v", err)
	}
	l1 := pcache.NewLocal(pcache.LocalConfig{})
	t.Cleanup(func() { _ = l1.Close() })
	svc := auth.NewService(users, sessions, rbac, pcache.NewTiered(l1, nil, pcache.TieredConfig{}), auth.DefaultConfig())

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
		Auth:      svc,
		Users:     testUserDir{repo: users},
	})
}

func postJSON(t *testing.T, h http.Handler, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getWith(t *testing.T, h http.Handler, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "np_session" && c.Value != "" {
			return c
		}
	}
	t.Fatal("no np_session cookie set")
	return nil
}

func TestAuthEndToEnd(t *testing.T) {
	h := newAuthRouter(t)

	// 1. Bootstrap the first admin.
	rec := postJSON(t, h, "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@example.com", "username": "admin", "password": "supersecret1"}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 2. /me without a session is 401.
	if rec := getWith(t, h, "/api/v1/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me (anon) = %d, want 401", rec.Code)
	}

	// 3. Login → sets cookie.
	rec = postJSON(t, h, "/api/v1/auth/login",
		map[string]string{"email": "admin@example.com", "password": "supersecret1"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	// 4. /me with the cookie returns the principal.
	rec = getWith(t, h, "/api/v1/auth/me", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d", rec.Code)
	}
	var me struct {
		Data struct {
			Email       string   `json:"email"`
			Permissions []string `json:"permissions"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Data.Email != "admin@example.com" {
		t.Fatalf("me email = %q", me.Data.Email)
	}

	// 5. Admin has "*" → /users (needs user.read) returns 200.
	if rec := getWith(t, h, "/api/v1/users", cookie); rec.Code != http.StatusOK {
		t.Fatalf("users (admin) = %d, want 200", rec.Code)
	}

	// 6. Logout revokes the session; /me is then 401 with the same cookie.
	if rec := postJSON(t, h, "/api/v1/auth/logout", nil, cookie); rec.Code != http.StatusOK {
		t.Fatalf("logout = %d", rec.Code)
	}
	if rec := getWith(t, h, "/api/v1/auth/me", cookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d, want 401", rec.Code)
	}
}

func TestBadCredentialsAndForbidden(t *testing.T) {
	h := newAuthRouter(t)

	// Bootstrap admin so a users table row exists.
	_ = postJSON(t, h, "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@example.com", "username": "admin", "password": "supersecret1"}, nil)

	// Wrong password → 401.
	if rec := postJSON(t, h, "/api/v1/auth/login",
		map[string]string{"email": "admin@example.com", "password": "nope"}, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", rec.Code)
	}

	// Second bootstrap → 409.
	if rec := postJSON(t, h, "/api/v1/auth/bootstrap",
		map[string]string{"email": "x@example.com", "username": "x", "password": "supersecret1"}, nil); rec.Code != http.StatusConflict {
		t.Fatalf("second bootstrap = %d, want 409", rec.Code)
	}
}

// TestSessionsEndToEnd drives the full session-management flow through the real
// router: two logins make two sessions; the caller sees both (its own flagged);
// revoke-others leaves only the current; the revoked session's cookie is dead.
func TestSessionsEndToEnd(t *testing.T) {
	h := newAuthRouter(t)
	_ = postJSON(t, h, "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@example.com", "username": "admin", "password": "supersecret1"}, nil)

	login := func() *http.Cookie {
		rec := postJSON(t, h, "/api/v1/auth/login",
			map[string]string{"email": "admin@example.com", "password": "supersecret1"}, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("login = %d", rec.Code)
		}
		return sessionCookie(t, rec)
	}
	c1 := login() // "current" for the assertions below
	c2 := login() // a second device

	type sess struct {
		UID     string `json:"uid"`
		Current bool   `json:"current"`
	}
	list := func(c *http.Cookie) []sess {
		rec := getWith(t, h, "/api/v1/auth/sessions", c)
		if rec.Code != http.StatusOK {
			t.Fatalf("list sessions = %d", rec.Code)
		}
		var out struct {
			Data struct {
				Sessions []sess `json:"sessions"`
			} `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out.Data.Sessions
	}

	// Both sessions are visible; exactly one is flagged current (per caller).
	s1 := list(c1)
	if len(s1) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(s1))
	}
	current := 0
	for _, s := range s1 {
		if s.Current {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("want exactly 1 current session, got %d", current)
	}

	// Sign out everywhere else from c1: c2 must stop working, c1 keeps working.
	if rec := postJSON(t, h, "/api/v1/auth/sessions/revoke-others", nil, c1); rec.Code != http.StatusOK {
		t.Fatalf("revoke-others = %d", rec.Code)
	}
	if rec := getWith(t, h, "/api/v1/auth/me", c2); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session still works = %d, want 401", rec.Code)
	}
	if rec := getWith(t, h, "/api/v1/auth/me", c1); rec.Code != http.StatusOK {
		t.Fatalf("current session broke = %d, want 200", rec.Code)
	}
	if got := list(c1); len(got) != 1 {
		t.Fatalf("after revoke-others, want 1 session, got %d", len(got))
	}
}
