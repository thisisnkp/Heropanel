package httpapi_test

import (
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
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/webserver"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

func deleteWith(t *testing.T, h http.Handler, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// fakeGateway satisfies broker.Gateway with successful no-op provisioning.
type fakeGateway struct{}

func (fakeGateway) Invoke(context.Context, string, any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}
func (fakeGateway) Health(context.Context) error { return nil }

func newSiteRouter(t *testing.T) http.Handler {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "site_e2e.db")
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
	sites := site.NewService(site.Deps{
		Repo:   repository.NewSiteStore(db),
		Broker: fakeGateway{},
		Web:    webserver.NewService(fakeGateway{}),
		PHP:    php.NewService(repository.NewPHPPoolStore(db), fakeGateway{}),
	})

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
		Sites:     sites,
	})
}

func TestSitesEndToEnd(t *testing.T) {
	h := newSiteRouter(t)

	// Bootstrap + login as admin (admin holds "*", so site.write/read pass).
	_ = postJSON(t, h, "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@example.com", "username": "admin", "password": "supersecret1"}, nil)
	rec := postJSON(t, h, "/api/v1/auth/login",
		map[string]string{"email": "admin@example.com", "password": "supersecret1"}, nil)
	cookie := sessionCookie(t, rec)

	// Anonymous create is rejected.
	if rec := postJSON(t, h, "/api/v1/sites",
		map[string]string{"name": "x", "primary_domain": "x.example.com"}, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon create = %d, want 401", rec.Code)
	}

	// Create a site.
	rec = postJSON(t, h, "/api/v1/sites",
		map[string]string{"name": "Acme", "primary_domain": "acme.example.com", "type": "static"}, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data site.Site `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.Status != site.StatusActive || created.Data.SystemUser == "" {
		t.Fatalf("unexpected created site: %+v", created.Data)
	}

	// List returns it.
	rec = getWith(t, h, "/api/v1/sites", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	var list struct {
		Data []site.Site `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data) != 1 {
		t.Fatalf("list len = %d, want 1", len(list.Data))
	}

	// Get by UID.
	if rec := getWith(t, h, "/api/v1/sites/"+created.Data.UID, cookie); rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}

	// Delete.
	if rec := deleteWith(t, h, "/api/v1/sites/"+created.Data.UID, cookie); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := getWith(t, h, "/api/v1/sites/"+created.Data.UID, cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", rec.Code)
	}
}
