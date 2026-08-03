package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/setup"
	"github.com/thisisnkp/heropanel/internal/tenancy"
	"github.com/thisisnkp/heropanel/internal/users"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

// newSetupRouter wires a router with the setup module backed by a real store, so
// the first-run wizard's HTTP surface can be exercised end to end.
func newSetupRouter(t *testing.T) http.Handler {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "setupapi.db")})
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
	ten := tenancy.NewResolver(uRepo, repository.NewResourceOwnerStore(db))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := config.Default()
	cfg.Security.RateLimit.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return httpapi.NewRouter(httpapi.Deps{
		Ctx:       ctx,
		Config:    cfg,
		Logger:    log,
		Version:   "test",
		StartedAt: time.Now(),
		Auth:      authSvc,
		UserMgmt:  users.NewService(uRepo, rbac, sRepo),
		Tenancy:   ten,
		Audit:     audit.NewService(repository.NewAuditRepository(db)),
		// Record-only: no provisioner, so Complete persists the selection without
		// touching a host — which is exactly the wizard's behavior in this slice.
		Setup: setup.NewService(repository.NewSetupStore(db), nil, log),
	})
}

// The first-run setup wizard end to end: a fresh panel reports setup incomplete,
// an admin reads the option catalogs, an unsupported backend is refused, a
// supported selection completes and provisions (record-only), and both GET
// /setup and /auth/status then report the panel configured.
func TestSetupAPIEndToEnd(t *testing.T) {
	h := newSetupRouter(t)

	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d", rec.Code)
	}
	adminCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil))

	// A fresh install reports setup incomplete via the public-ish status route.
	if got := statusSetupComplete(t, h, adminCookie); got {
		t.Fatal("fresh install should report setup_complete=false")
	}

	// The wizard reads its option catalogs and the (empty) current state.
	rec := getWith(t, h, "/api/v1/setup", adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("get setup = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Data struct {
			State      setup.State    `json:"state"`
			Webservers []setup.Option `json:"webservers"`
			DBEngines  []setup.Option `json:"db_engines"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Data.State.Completed {
		t.Error("state should be incomplete before submit")
	}
	if len(got.Data.Webservers) == 0 || len(got.Data.DBEngines) == 0 {
		t.Fatal("expected option catalogs")
	}

	// An unknown webserver (not in the catalog) is refused (validation → 400).
	if rec := doJSON(t, h, "POST", "/api/v1/setup",
		map[string]any{"webserver": "iis", "db_engine": "mariadb"}, adminCookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown webserver = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	// A supported selection completes.
	rec = doJSON(t, h, "POST", "/api/v1/setup",
		map[string]any{"webserver": "openlitespeed", "db_engine": "mariadb", "manage_dns": true, "create_mail": false}, adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete setup = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var done struct {
		Data setup.State `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &done)
	if !done.Data.Completed || done.Data.CompletedAt == nil {
		t.Fatalf("expected completed state, got %+v", done.Data)
	}
	if done.Data.Webserver != setup.WebserverOpenLiteSpeed || !done.Data.ManageDNS {
		t.Fatalf("selection not echoed: %+v", done.Data)
	}

	// Both status surfaces now report the panel configured.
	if got := statusSetupComplete(t, h, adminCookie); !got {
		t.Fatal("after completion status should report setup_complete=true")
	}
	rec = getWith(t, h, "/api/v1/setup", adminCookie)
	var after struct {
		Data struct {
			State setup.State `json:"state"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &after)
	if !after.Data.State.Completed {
		t.Fatal("persisted state should be completed")
	}
}

// A user without setup.manage cannot read or drive the wizard.
func TestSetupRequiresPermission(t *testing.T) {
	h := newSetupRouter(t)
	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d", rec.Code)
	}
	// A developer (no setup.manage) is created and gets a 403 on the wizard.
	adminCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil))
	if rec := doJSON(t, h, "POST", "/api/v1/users",
		map[string]any{"email": "dev@h.io", "username": "dev", "password": "supersecret1", "roles": []string{"developer"}}, adminCookie); rec.Code != http.StatusCreated {
		t.Fatalf("create dev = %d: %s", rec.Code, rec.Body.String())
	}
	devCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "dev@h.io", "password": "supersecret1"}, nil))
	if rec := getWith(t, h, "/api/v1/setup", devCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("developer get setup = %d, want 403", rec.Code)
	}
}

// statusSetupComplete reads /auth/status and returns its setup_complete flag.
func statusSetupComplete(t *testing.T, h http.Handler, cookie *http.Cookie) bool {
	t.Helper()
	rec := getWith(t, h, "/api/v1/auth/status", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth/status = %d: %s", rec.Code, rec.Body.String())
	}
	var st struct {
		Data struct {
			SetupComplete bool `json:"setup_complete"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	return st.Data.SetupComplete
}
