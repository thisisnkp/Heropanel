package httpapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/thisisnkp/nexpanel/internal/license"
)

// What a lapsed licence does to the panel's API, driven through the real router.
//
// The assertions come in pairs on purpose. For every route that closes there is
// one that must stay open, because the failure this file exists to catch is not
// "the lock does not work" — it is the lock working too well and taking
// somebody's hosting with it.

// licensedRouter builds the full router with a licence in a chosen state.
//
// The state is produced by writing a real, correctly signed lease with a chosen
// expiry, not by stubbing the service: the whole point is that the ladder, the
// store and the router agree, and a fake service would only prove the fake.
func licensedRouter(t *testing.T, expiry time.Time, activated bool) http.Handler {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	svc, err := license.New(license.Options{
		Dir:       dir,
		ServerURL: "http://127.0.0.1:1", // nothing listens; nothing here needs it
		ExtraKeys: map[string]string{"lk-test": base64.StdEncoding.EncodeToString(pub)},
		Version:   "0.0.0-test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	if activated {
		claims := license.Claims{
			LID:   "lic_e2e",
			Plan:  "pro",
			Feat:  []string{"docker", "mail"},
			Lim:   license.Limits{Sites: 50, DBs: 100, Users: 10},
			FP:    "sha256:e2e",
			IAT:   time.Now().Add(-8 * 24 * time.Hour).Unix(),
			Exp:   expiry.Unix(),
			Grace: int64(license.DefaultGrace / time.Second),
			State: "active",
		}
		store, err := license.NewStore(dir, license.Keyring{"lk-test": pub})
		if err != nil {
			t.Fatal(err)
		}
		// The lease is dated in the past, so `now` is left alone: the ladder is
		// reached by moving the expiry, never by moving the clock.
		if err := store.SaveActivation(
			signLease(priv, claims), claims.LID, claims.FP, "nxi_e2e", claims,
			time.Unix(claims.IAT, 0),
		); err != nil {
			t.Fatal(err)
		}
	}

	d := fullRouterDeps(t)
	d.License = svc
	return NewRouter(d)
}

func signLease(priv ed25519.PrivateKey, claims license.Claims) string {
	header, _ := json.Marshal(map[string]string{"alg": license.Alg, "typ": "JWT", "kid": "lk-test"})
	payload, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	return signing + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, []byte(signing)))
}

func do(t *testing.T, r http.Handler, method, path string, perms ...string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, asPrincipal(method, path, perms...))
	return rec
}

// A current licence gates nothing.
func TestCurrentLicenceGatesNothing(t *testing.T) {
	r := licensedRouter(t, time.Now().Add(7*24*time.Hour), true)

	for _, route := range []struct{ method, path, perm string }{
		{"GET", "/api/v1/sites", "site.read"},
		{"POST", "/api/v1/sites", "site.write"},
		{"POST", "/api/v1/databases", "database.write"},
		{"POST", "/api/v1/docker/images/pull", "docker.write"},
	} {
		rec := do(t, r, route.method, route.path, route.perm)
		if rec.Code == http.StatusPaymentRequired {
			t.Fatalf("%s %s was refused for licence reasons on a current licence", route.method, route.path)
		}
	}
}

// Degraded blocks creation and nothing else. The paired read on every line is
// the assertion that matters: an operator whose card failed must still be able
// to see, manage and fix everything they already run.
func TestDegradedBlocksCreationOnly(t *testing.T) {
	// Twenty days past expiry: past the fortnight of grace, inside the degraded
	// window.
	r := licensedRouter(t, time.Now().Add(-20*24*time.Hour), true)

	blocked := []struct{ method, path, perm string }{
		{"POST", "/api/v1/sites", "site.write"},
		{"POST", "/api/v1/databases", "database.write"},
		{"POST", "/api/v1/users", "user.write"},
		{"POST", "/api/v1/docker/containers", "docker.write"},
		{"POST", "/api/v1/docker/images/pull", "docker.write"},
	}
	for _, route := range blocked {
		rec := do(t, r, route.method, route.path, route.perm)
		if rec.Code != http.StatusPaymentRequired {
			t.Fatalf("%s %s: status %d, want 402", route.method, route.path, rec.Code)
		}
	}

	// Everything else keeps working. These are the routes an operator uses to
	// run the sites they already have — and the backup ones, which must never
	// stop, because a lapsed licence is exactly when somebody is about to move
	// their data somewhere else.
	open := []struct{ method, path, perm string }{
		{"GET", "/api/v1/sites", "site.read"},
		{"GET", "/api/v1/databases", "database.read"},
		{"GET", "/api/v1/users", "user.read"},
		{"GET", "/api/v1/backups", "backup.read"},
		{"POST", "/api/v1/docker/containers/x/restart", "docker.write"},
		{"GET", "/api/v1/system/license", "system.read"},
	}
	for _, route := range open {
		rec := do(t, r, route.method, route.path, route.perm)
		if rec.Code == http.StatusPaymentRequired {
			t.Fatalf("%s %s was blocked in the degraded state; only *creating* things should stop",
				route.method, route.path)
		}
	}
}

// Locked narrows the panel API to the licence routes and the ones needed to
// reach them. It still stops no service — that is asserted where it can be, in
// what this router does *not* touch.
func TestLockedNarrowsThePanelToReactivation(t *testing.T) {
	r := licensedRouter(t, time.Now().Add(-40*24*time.Hour), true)

	for _, route := range []struct{ method, path, perm string }{
		{"GET", "/api/v1/sites", "site.read"},
		{"POST", "/api/v1/sites", "site.write"},
		{"GET", "/api/v1/databases", "database.read"},
		{"GET", "/api/v1/system/update", "system.read"},
	} {
		rec := do(t, r, route.method, route.path, route.perm)
		if rec.Code != http.StatusPaymentRequired {
			t.Fatalf("%s %s: status %d, want 402 while locked", route.method, route.path, rec.Code)
		}
	}

	// Without these there is no way back: no licence screen, and no way to sign
	// in to reach it.
	for _, route := range []struct{ method, path, perm string }{
		{"GET", "/api/v1/system/license", "system.read"},
		{"POST", "/api/v1/system/license/activate", "system.write"},
		{"POST", "/api/v1/system/license/refresh", "system.write"},
		{"GET", "/api/v1/system/info", ""},
	} {
		rec := do(t, r, route.method, route.path, route.perm)
		if rec.Code == http.StatusPaymentRequired {
			t.Fatalf("%s %s was locked out; there would be no way to reactivate", route.method, route.path)
		}
	}
	// Auth has no permission requirement and must answer, or nobody can sign in
	// to fix it.
	if rec := do(t, r, "GET", "/api/v1/auth/status"); rec.Code == http.StatusPaymentRequired {
		t.Fatal("sign-in was locked out")
	}
}

// Authorisation is answered before payment. A caller who was never allowed must
// be told so — "renew your licence" would be misleading, and it would leak the
// installation's commercial state to anyone who can reach the API.
func TestPermissionIsAnsweredBeforeLicence(t *testing.T) {
	r := licensedRouter(t, time.Now().Add(-40*24*time.Hour), true)

	if rec := do(t, r, "POST", "/api/v1/sites"); rec.Code != http.StatusForbidden {
		t.Fatalf("no permissions on a locked panel: status %d, want 403", rec.Code)
	}
	if rec := do(t, r, "POST", "/api/v1/sites", "site.write"); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("with the permission: status %d, want 402", rec.Code)
	}
}

// A never-activated install is locked, and the licence screen says so in words
// that send the operator to the key field rather than to billing.
func TestFreshInstallIsLockedButExplains(t *testing.T) {
	r := licensedRouter(t, time.Time{}, false)

	rec := do(t, r, "GET", "/api/v1/system/license", "system.read")
	if rec.Code != http.StatusOK {
		t.Fatalf("the licence screen must always answer: status %d", rec.Code)
	}
	// Responses are wrapped in the panel's {data, meta} envelope.
	var body struct {
		Data struct {
			State     string `json:"state"`
			Activated bool   `json:"activated"`
			Banner    string `json:"banner"`
			Enforced  bool   `json:"enforced"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	view := body.Data
	if view.State != string(license.StateLocked) || view.Activated {
		t.Fatalf("view = %+v, want locked and not activated", view)
	}
	if view.Banner == "" {
		t.Fatal("no banner on a fresh install — the operator is told nothing")
	}
	// A build with no compiled-in key enforces nothing, and says so rather than
	// showing a tick it did not earn.
	if view.Enforced {
		t.Fatal("a build with a configured (not pinned) key reported itself as enforcing")
	}
}

// The refusal has to reassure. This is the message a frightened customer reads
// at 2am, and "your licence has lapsed" alone reads as "my sites are down".
func TestRefusalsSayTheSitesAreFine(t *testing.T) {
	r := licensedRouter(t, time.Now().Add(-40*24*time.Hour), true)

	rec := do(t, r, "POST", "/api/v1/sites", "site.write")
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal is not the standard envelope: %s", rec.Body.String())
	}
	if body.Error.Code == "" {
		t.Fatal("the refusal carries no machine code")
	}
	if !containsFold(body.Error.Message, "unaffected") && !containsFold(body.Error.Message, "running") {
		t.Fatalf("the refusal does not reassure about services: %q", body.Error.Message)
	}
}

func containsFold(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
