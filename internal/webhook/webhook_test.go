package webhook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/audit"
	"github.com/thisisnkp/nexpanel/internal/auth"
	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/internal/tenancy"
	"github.com/thisisnkp/nexpanel/pkg/secrets"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newEnv(t *testing.T) (*Service, *repository.WebhookStore, *repository.DB, *secrets.Cipher) {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "wh.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rbac := repository.NewRBACRepository(db)
	if err := auth.SeedRBAC(context.Background(), rbac); err != nil {
		t.Fatalf("seed: %v", err)
	}
	key, _ := secrets.GenerateMasterKey()
	cipher, _ := secrets.FromBase64(key)
	store := repository.NewWebhookStore(db, cipher)
	ten := tenancy.NewResolver(repository.NewUserRepository(db), repository.NewResourceOwnerStore(db))
	return NewService(store, ten, rbac, testLogger()), store, db, cipher
}

// mkUser inserts a user (optionally under a parent, optionally admin) and returns id.
func mkUser(t *testing.T, db *repository.DB, email string, parentID int64, admin bool) int64 {
	t.Helper()
	repo := repository.NewUserRepository(db)
	u := &repository.User{Email: email, Username: email, DisplayName: email, Status: "active"}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if parentID != 0 {
		if err := repo.SetParent(context.Background(), u.ID, parentID); err != nil {
			t.Fatal(err)
		}
	}
	if admin {
		if err := repository.NewRBACRepository(db).AssignRole(context.Background(), u.ID, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	return u.ID
}

func TestSign(t *testing.T) {
	a := Sign("secret", "1700000000", `{"x":1}`)
	b := Sign("secret", "1700000000", `{"x":1}`)
	if a != b {
		t.Fatal("Sign must be deterministic")
	}
	if Sign("other", "1700000000", `{"x":1}`) == a {
		t.Fatal("a different secret must produce a different signature")
	}
	if len(a) < 8 || a[:7] != "sha256=" {
		t.Fatalf("unexpected signature format: %s", a)
	}
}

// A superuser's webhook receives a matching event and the dispatcher delivers it
// with a valid HMAC signature; the delivery is then recorded as delivered.
func TestOnAuditEntryDeliversSigned(t *testing.T) {
	svc, store, db, _ := newEnv(t)
	ctx := context.Background()
	adminID := mkUser(t, db, "admin@h.io", 0, true)

	var (
		mu      sync.Mutex
		gotBody string
		gotSig  string
		gotTS   string
		gotEv   string
		hits    int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits++
		gotBody = string(b)
		gotSig = r.Header.Get(headerSignature)
		gotTS = r.Header.Get(headerTimestamp)
		gotEv = r.Header.Get(headerEvent)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	created, err := svc.Create(ctx, adminID, srv.URL, []string{"sites"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A non-matching event enqueues nothing.
	svc.OnAuditEntry(audit.Entry{UID: "e0", ResourceType: "dns", ResourceID: "z1", Outcome: audit.OutcomeSuccess, ActorUserID: adminID, CreatedAt: "t"})
	// A matching event enqueues one delivery.
	svc.OnAuditEntry(audit.Entry{UID: "e1", Action: "POST /api/v1/sites", ResourceType: "sites", ResourceID: "s1", Outcome: audit.OutcomeSuccess, ActorUserID: adminID, CreatedAt: "t", Detail: `{"primary_domain":"x.com"}`})

	wh, _ := store.GetWebhookForOwner(ctx, created.UID, nil)
	NewDispatcher(store, testLogger()).drain(ctx)

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("endpoint hit %d times, want exactly 1 (only the matching event)", hits)
	}
	if gotEv != "sites.success" {
		t.Fatalf("event header = %q, want sites.success", gotEv)
	}
	if Sign(created.Secret, gotTS, gotBody) != gotSig {
		t.Fatalf("signature did not verify")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if body["resource_type"] != "sites" || body["resource_id"] != "s1" {
		t.Fatalf("payload wrong: %v", body)
	}
	// The delivery is recorded as delivered.
	dels, _ := store.ListDeliveries(ctx, wh.ID, 10)
	if len(dels) != 1 || dels[0].Status != "delivered" {
		t.Fatalf("deliveries = %+v, want one delivered", dels)
	}
}

// A reseller's webhook only receives events actored inside its tenant subtree.
func TestTenancyFiltersDeliveries(t *testing.T) {
	svc, store, db, _ := newEnv(t)
	ctx := context.Background()
	resellerID := mkUser(t, db, "res@h.io", 0, false)
	clientID := mkUser(t, db, "client@h.io", resellerID, false)
	outsiderID := mkUser(t, db, "out@h.io", 0, false)

	created, err := svc.Create(ctx, resellerID, "https://example.invalid/hook", []string{"*"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	row, _ := store.GetWebhookForOwner(ctx, created.UID, nil)

	// Event by the reseller's client → enqueued.
	svc.OnAuditEntry(audit.Entry{UID: "e1", ResourceType: "sites", ResourceID: "s1", Outcome: audit.OutcomeSuccess, ActorUserID: clientID, CreatedAt: "t"})
	// Event by an outsider → not enqueued.
	svc.OnAuditEntry(audit.Entry{UID: "e2", ResourceType: "sites", ResourceID: "s2", Outcome: audit.OutcomeSuccess, ActorUserID: outsiderID, CreatedAt: "t"})
	// A denied outcome is never broadcast.
	svc.OnAuditEntry(audit.Entry{UID: "e3", ResourceType: "sites", ResourceID: "s3", Outcome: audit.OutcomeDenied, ActorUserID: clientID, CreatedAt: "t"})

	dels, _ := store.ListDeliveries(ctx, row.ID, 10)
	if len(dels) != 1 || dels[0].ResourceID != "s1" {
		t.Fatalf("deliveries = %+v, want only the in-tenant s1", dels)
	}
}

// A failing endpoint is retried: the delivery stays pending with a future next
// attempt, not marked delivered.
func TestFailedDeliveryRetries(t *testing.T) {
	svc, store, db, _ := newEnv(t)
	ctx := context.Background()
	adminID := mkUser(t, db, "admin@h.io", 0, true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	created, _ := svc.Create(ctx, adminID, srv.URL, []string{"*"})
	row, _ := store.GetWebhookForOwner(ctx, created.UID, nil)
	svc.OnAuditEntry(audit.Entry{UID: "e1", ResourceType: "sites", ResourceID: "s1", Outcome: audit.OutcomeSuccess, ActorUserID: adminID, CreatedAt: "t"})

	NewDispatcher(store, testLogger()).drain(ctx)

	dels, _ := store.ListDeliveries(ctx, row.ID, 10)
	if len(dels) != 1 {
		t.Fatalf("want 1 delivery, got %d", len(dels))
	}
	if dels[0].Status != "pending" || dels[0].Attempts != 1 || dels[0].ResponseCode != 500 {
		t.Fatalf("failed delivery state = %+v, want pending/attempts=1/code=500", dels[0])
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	if backoffFor(1) != baseBackoff {
		t.Fatalf("first backoff = %v, want %v", backoffFor(1), baseBackoff)
	}
	if backoffFor(2) != 2*baseBackoff {
		t.Fatalf("second backoff = %v, want %v", backoffFor(2), 2*baseBackoff)
	}
	if backoffFor(20) != maxBackoff {
		t.Fatalf("large attempt backoff = %v, want cap %v", backoffFor(20), maxBackoff)
	}
}
