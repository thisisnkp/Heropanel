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
	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/tenancy"
	"github.com/thisisnkp/heropanel/internal/users"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

// newTenantRouter wires Auth + UserMgmt + Sites (read paths only) + a real
// tenancy resolver over one SQLite DB, and hands back the db and site store so a
// test can seed users and sites with explicit owners.
func newTenantRouter(t *testing.T) (http.Handler, *repository.DB, *repository.SiteStore) {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "ten.db")})
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
	// Give the reseller role the permissions its route gates require; tenancy
	// then constrains *which* users/sites it may touch.
	for _, perm := range []string{"user.read", "user.write", "site.read", "site.write", "dns.read", "dns.write"} {
		if err := rbac.GrantPermission(context.Background(), "reseller", perm); err != nil {
			t.Fatalf("grant %s: %v", perm, err)
		}
	}
	siteStore := repository.NewSiteStore(db)
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
		Tenancy:   tenancy.NewResolver(uRepo, repository.NewResourceOwnerStore(db)),
		Sites:     site.NewService(site.Deps{Repo: siteStore}),
		DNS:       dns.NewService(repository.NewDNSStore(db), nil),
	})
	return h, db, siteStore
}

func seedZone(t *testing.T, db *repository.DB, ownerID int64, name string) string {
	t.Helper()
	z := &dns.ZoneRow{
		OwnerID: ownerID, Name: name, PrimaryNS: "ns1." + name,
		AdminEmail: "hostmaster." + name, Serial: 1, TTL: 3600, Status: "active",
	}
	if err := repository.NewDNSStore(db).InsertZone(context.Background(), z); err != nil {
		t.Fatalf("seed zone %s: %v", name, err)
	}
	return z.UID
}

func seedSite(t *testing.T, store *repository.SiteStore, ownerID int64, domain string) string {
	t.Helper()
	rec := &site.Record{
		OwnerID: ownerID, Name: domain, PrimaryDomain: domain,
		Type: "static", DeployMode: "baremetal", Status: "active", Webserver: "openlitespeed",
	}
	if err := store.Insert(context.Background(), rec); err != nil {
		t.Fatalf("seed site %s: %v", domain, err)
	}
	return rec.UID
}

func siteDomains(t *testing.T, rec *httptest.ResponseRecorder) map[string]bool {
	t.Helper()
	var env struct {
		Data []struct {
			UID           string `json:"uid"`
			PrimaryDomain string `json:"primary_domain"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode sites: %v (%s)", err, rec.Body.String())
	}
	out := map[string]bool{}
	for _, s := range env.Data {
		out[s.PrimaryDomain] = true
	}
	return out
}

// The headline exit criterion, end to end: a reseller sees and reaches only its
// own tenant's sites (itself plus its clients), never another tenant's — and an
// admin sees everything.
func TestSiteTenantIsolationEndToEnd(t *testing.T) {
	h, db, store := newTenantRouter(t)
	ctx := context.Background()
	uRepo := repository.NewUserRepository(db)

	// Admin.
	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d: %s", rec.Code, rec.Body.String())
	}
	adminCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil))

	// Admin creates a reseller and a second, independent top-level owner.
	mkUser := func(email, username string, roles []string, cookie *http.Cookie) {
		rec := doJSON(t, h, "POST", "/api/v1/users", map[string]any{
			"email": email, "username": username, "password": "supersecret1", "roles": roles,
		}, cookie)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", email, rec.Code, rec.Body.String())
		}
	}
	mkUser("reseller@h.io", "reseller", []string{"reseller"}, adminCookie)
	mkUser("other@h.io", "other", []string{"reseller"}, adminCookie)

	resellerCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "reseller@h.io", "password": "supersecret1"}, nil))

	// The reseller creates a client of its own — it lands in the reseller's tenant.
	mkUser("client@h.io", "client", nil, resellerCookie)

	reseller, _ := uRepo.GetByEmail(ctx, "reseller@h.io")
	client, _ := uRepo.GetByEmail(ctx, "client@h.io")
	other, _ := uRepo.GetByEmail(ctx, "other@h.io")

	// Seed one site per owner.
	_ = seedSite(t, store, reseller.ID, "reseller.example.com")
	_ = seedSite(t, store, client.ID, "client.example.com")
	otherSiteUID := seedSite(t, store, other.ID, "other.example.com")

	// The reseller's site list is its whole subtree — its own site and its
	// client's — and nothing from the other tenant.
	got := siteDomains(t, getWith(t, h, "/api/v1/sites", resellerCookie))
	if !got["reseller.example.com"] || !got["client.example.com"] {
		t.Fatalf("reseller list missing its tenant: %v", got)
	}
	if got["other.example.com"] {
		t.Fatalf("reseller list leaks another tenant's site: %v", got)
	}

	// Reaching the other tenant's site directly is a 404 — indistinguishable from
	// "no such site", so the boundary discloses nothing.
	if rec := getWith(t, h, "/api/v1/sites/"+otherSiteUID, resellerCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("reseller GET other's site = %d, want 404", rec.Code)
	}

	// The admin (superuser) sees every site.
	all := siteDomains(t, getWith(t, h, "/api/v1/sites", adminCookie))
	if !all["reseller.example.com"] || !all["client.example.com"] || !all["other.example.com"] {
		t.Fatalf("admin should see all sites: %v", all)
	}
}

// The same isolation holds for a second, differently-shaped resource (DNS zones,
// reached through the generalized guard and the generic owner lookup), proving
// the primitive is not sites-specific.
func TestDNSTenantIsolationEndToEnd(t *testing.T) {
	h, db, _ := newTenantRouter(t)
	ctx := context.Background()
	uRepo := repository.NewUserRepository(db)

	if rec := doJSON(t, h, "POST", "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@h.io", "username": "admin", "password": "supersecret1"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d", rec.Code)
	}
	adminCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "admin@h.io", "password": "supersecret1"}, nil))

	mk := func(email, username string) {
		rec := doJSON(t, h, "POST", "/api/v1/users", map[string]any{
			"email": email, "username": username, "password": "supersecret1", "roles": []string{"reseller"},
		}, adminCookie)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", email, rec.Code, rec.Body.String())
		}
	}
	mk("reseller@h.io", "reseller")
	mk("other@h.io", "othertenant")
	resellerCookie := sessionCookie(t, doJSON(t, h, "POST", "/api/v1/auth/login",
		map[string]string{"email": "reseller@h.io", "password": "supersecret1"}, nil))

	reseller, _ := uRepo.GetByEmail(ctx, "reseller@h.io")
	other, _ := uRepo.GetByEmail(ctx, "other@h.io")
	_ = seedZone(t, db, reseller.ID, "mine.example.com")
	otherZoneUID := seedZone(t, db, other.ID, "theirs.example.com")

	decodeZones := func(rec *httptest.ResponseRecorder) map[string]bool {
		var env struct {
			Data []struct {
				Name string `json:"name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode zones: %v (%s)", err, rec.Body.String())
		}
		out := map[string]bool{}
		for _, z := range env.Data {
			out[z.Name] = true
		}
		return out
	}

	// The reseller's zone list shows only its own zone.
	got := decodeZones(getWith(t, h, "/api/v1/dns/zones", resellerCookie))
	if !got["mine.example.com"] || got["theirs.example.com"] {
		t.Fatalf("reseller zone list = %v, want only its own", got)
	}
	// Reaching the other tenant's zone directly is a 404.
	if rec := getWith(t, h, "/api/v1/dns/zones/"+otherZoneUID, resellerCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("reseller GET other's zone = %d, want 404", rec.Code)
	}
	// The admin sees both zones.
	if all := decodeZones(getWith(t, h, "/api/v1/dns/zones", adminCookie)); !all["mine.example.com"] || !all["theirs.example.com"] {
		t.Fatalf("admin should see both zones: %v", all)
	}
}
