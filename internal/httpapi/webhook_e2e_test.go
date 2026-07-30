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
	"github.com/thisisnkp/heropanel/internal/tenancy"
	"github.com/thisisnkp/heropanel/internal/users"
	"github.com/thisisnkp/heropanel/internal/webhook"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
	"github.com/thisisnkp/heropanel/pkg/secrets"
)

func newWebhookRouter(t *testing.T) http.Handler {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "whapi.db")})
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
	key, _ := secrets.GenerateMasterKey()
	cipher, _ := secrets.FromBase64(key)
	l1 := pcache.NewLocal(pcache.LocalConfig{})
	t.Cleanup(func() { _ = l1.Close() })
	authSvc := auth.NewService(uRepo, sRepo, rbac, pcache.NewTiered(l1, nil, pcache.TieredConfig{}), auth.DefaultConfig())
	ten := tenancy.NewResolver(uRepo, repository.NewResourceOwnerStore(db))
	whStore := repository.NewWebhookStore(db, cipher)

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
		Tenancy:   ten,
		Audit:     audit.NewService(repository.NewAuditRepository(db)),
		Webhooks:  webhook.NewService(whStore, ten, rbac, slog.New(slog.NewTextHandler(io.Discard, nil))),
	})
}

// The webhook HTTP surface end to end: an admin creates a subscription (the
// signing secret comes back exactly once), it lists, its deliveries endpoint
// answers, and it deletes; a user without the webhook permission is refused.
func TestWebhookAPIEndToEnd(t *testing.T) {
	h := newWebhookRouter(t)

	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d", rec.Code)
	}
	adminCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil))

	// Create — the secret is present once.
	rec := doJSON(t, h, "POST", "/api/v1/webhooks", map[string]any{
		"url": "https://example.com/hook", "events": []string{"sites", "dns"},
	}, adminCookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			UID    string   `json:"uid"`
			Secret string   `json:"secret"`
			Events []string `json:"events"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.UID == "" || created.Data.Secret == "" {
		t.Fatalf("create response missing uid/secret: %s", rec.Body.String())
	}

	// List — never returns the secret.
	rec = getWith(t, h, "/api/v1/webhooks", adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	var list struct {
		Data struct {
			Webhooks []struct {
				UID    string `json:"uid"`
				Secret string `json:"secret"`
			} `json:"webhooks"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data.Webhooks) != 1 || list.Data.Webhooks[0].UID != created.Data.UID {
		t.Fatalf("list = %+v, want the one webhook", list.Data.Webhooks)
	}
	if list.Data.Webhooks[0].Secret != "" {
		t.Fatal("the secret must never be returned by list")
	}

	// Deliveries endpoint answers (empty is fine).
	if rec := getWith(t, h, "/api/v1/webhooks/"+created.Data.UID+"/deliveries", adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("deliveries = %d: %s", rec.Code, rec.Body.String())
	}

	// A developer (no webhook permission) is refused create.
	if rec := doJSON(t, h, "POST", "/api/v1/users", map[string]any{
		"email": "dev@h.io", "username": "dev", "password": "supersecret1", "roles": []string{"developer"},
	}, adminCookie); rec.Code != http.StatusCreated {
		t.Fatalf("create dev = %d: %s", rec.Code, rec.Body.String())
	}
	devCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "dev@h.io", "password": "supersecret1"}, nil))
	if rec := doJSON(t, h, "POST", "/api/v1/webhooks", map[string]any{"url": "https://x.com/h"}, devCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("dev create webhook = %d, want 403", rec.Code)
	}

	// Delete.
	if rec := doJSON(t, h, "DELETE", "/api/v1/webhooks/"+created.Data.UID, nil, adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}
	rec = getWith(t, h, "/api/v1/webhooks", adminCookie)
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data.Webhooks) != 0 {
		t.Fatalf("after delete, list should be empty, got %d", len(list.Data.Webhooks))
	}
}
