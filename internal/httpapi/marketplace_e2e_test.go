package httpapi_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
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
	"github.com/thisisnkp/heropanel/internal/marketplace"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/tenancy"
	"github.com/thisisnkp/heropanel/internal/users"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
	"github.com/thisisnkp/heropanel/pkg/proto"
)

// mktManifest is a valid module manifest for the marketplace e2e.
func mktManifest(slug, name string) proto.Manifest {
	return proto.Manifest{
		APIVersion: proto.APIVersion,
		Kind:       "Module",
		Metadata:   proto.Metadata{Slug: slug, Name: name, Version: "1.0.0", Category: "backup"},
		Spec: proto.Spec{
			Binary:       "hp-mod-" + slug,
			Capabilities: []string{"backup.offsite"},
			Arch:         []string{"amd64"},
			Signing:      proto.Signing{Checksum: "abc123"},
		},
	}
}

// newMarketplaceRouter wires a router whose marketplace trusts one publisher key
// and offers one signed module ("backups-pro") plus one unsigned one ("rogue").
func newMarketplaceRouter(t *testing.T) http.Handler {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mktapi.db")})
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

	pub, priv, _ := marketplace.GenerateKey()
	sk, _ := base64.StdEncoding.DecodeString(priv)
	signed := mktManifest("backups-pro", "Backups Pro")
	sig, _ := marketplace.SignManifest(signed, ed25519.PrivateKey(sk))
	signed.Spec.Signing.Signature = sig
	rogue := mktManifest("rogue", "Rogue") // unsigned

	kr, _ := marketplace.NewKeyring(pub)
	cat := &marketplace.Catalog{Modules: []proto.Manifest{signed, rogue}}
	mkt := marketplace.NewService(kr, cat, repository.NewModuleStore(db), slog.New(slog.NewTextHandler(io.Discard, nil)))

	cfg := config.Default()
	cfg.Security.RateLimit.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return httpapi.NewRouter(httpapi.Deps{
		Ctx:         ctx,
		Config:      cfg,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:     "test",
		StartedAt:   time.Now(),
		Auth:        authSvc,
		UserMgmt:    users.NewService(uRepo, rbac, sRepo),
		Tenancy:     ten,
		Audit:       audit.NewService(repository.NewAuditRepository(db)),
		Marketplace: mkt,
	})
}

// The marketplace HTTP surface end to end: an admin browses (each entry carries a
// trust verdict), installs the signed module, is refused the unsigned one, toggles
// enable/disable, and uninstalls; a developer without module.read is refused.
func TestMarketplaceAPIEndToEnd(t *testing.T) {
	h := newMarketplaceRouter(t)

	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d", rec.Code)
	}
	adminCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil))

	// Browse — the catalog reports one verified and one unverified module.
	rec := getWith(t, h, "/api/v1/marketplace/catalog", adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog = %d: %s", rec.Code, rec.Body.String())
	}
	var cat struct {
		Data struct {
			TrustAnchored bool                       `json:"trust_anchored"`
			Modules       []marketplace.CatalogEntry `json:"modules"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	if !cat.Data.TrustAnchored {
		t.Error("trust_anchored should be true with a pinned key")
	}
	verdict := map[string]marketplace.CatalogEntry{}
	for _, e := range cat.Data.Modules {
		verdict[e.Slug] = e
	}
	if !verdict["backups-pro"].Verified {
		t.Errorf("signed module not verified: %+v", verdict["backups-pro"])
	}
	if verdict["rogue"].Verified {
		t.Error("unsigned module reported verified")
	}

	// Install the unsigned module — refused with 403.
	if rec := doJSON(t, h, "POST", "/api/v1/marketplace/modules/rogue/install", nil, adminCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("install rogue = %d, want 403: %s", rec.Code, rec.Body.String())
	}

	// Install the signed module — 201.
	if rec := doJSON(t, h, "POST", "/api/v1/marketplace/modules/backups-pro/install", nil, adminCookie); rec.Code != http.StatusCreated {
		t.Fatalf("install backups-pro = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	// It now shows installed in the catalog and in the inventory.
	rec = getWith(t, h, "/api/v1/marketplace/installed", adminCookie)
	var inv struct {
		Data struct {
			Modules []marketplace.InstalledModule `json:"modules"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &inv)
	if len(inv.Data.Modules) != 1 || inv.Data.Modules[0].Slug != "backups-pro" {
		t.Fatalf("inventory = %+v, want the one installed module", inv.Data.Modules)
	}
	if inv.Data.Modules[0].State != marketplace.StateInstalled {
		t.Errorf("fresh install state = %q", inv.Data.Modules[0].State)
	}

	// Enable then disable.
	if rec := doJSON(t, h, "POST", "/api/v1/marketplace/modules/backups-pro/enable", nil, adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, "POST", "/api/v1/marketplace/modules/backups-pro/disable", nil, adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("disable = %d", rec.Code)
	}

	// Uninstall.
	if rec := doJSON(t, h, "DELETE", "/api/v1/marketplace/modules/backups-pro", nil, adminCookie); rec.Code != http.StatusOK {
		t.Fatalf("uninstall = %d", rec.Code)
	}
	rec = getWith(t, h, "/api/v1/marketplace/installed", adminCookie)
	_ = json.Unmarshal(rec.Body.Bytes(), &inv)
	if len(inv.Data.Modules) != 0 {
		t.Fatalf("after uninstall inventory should be empty, got %d", len(inv.Data.Modules))
	}

	// A developer without module.read is refused browsing.
	if rec := doJSON(t, h, "POST", "/api/v1/users", map[string]any{
		"email": "dev@h.io", "username": "dev", "password": "supersecret1", "roles": []string{"developer"},
	}, adminCookie); rec.Code != http.StatusCreated {
		t.Fatalf("create dev = %d: %s", rec.Code, rec.Body.String())
	}
	devCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "dev@h.io", "password": "supersecret1"}, nil))
	if rec := getWith(t, h, "/api/v1/marketplace/catalog", devCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("dev browse = %d, want 403", rec.Code)
	}
}
