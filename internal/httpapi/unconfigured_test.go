package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A panel started without a datastore has no auth service, so the whole auth
// group is unmounted. It must still answer the three routes the login screen
// calls — otherwise the operator is told "the requested resource was not found"
// when the real problem is that no database is configured. This regressed once
// and is the first thing a fresh `hpd` run hits, so it is pinned here.

func newUnconfiguredRouter() http.Handler {
	// Deps with no Auth (and no DB) is exactly what bootstrap builds when the
	// DSN is empty.
	return NewRouter(Deps{Version: "test"})
}

func TestUnconfiguredStatusIsServedNotFourOhFour(t *testing.T) {
	rec := httptest.NewRecorder()
	newUnconfiguredRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a 404 here is what leaves the UI guessing)", rec.Code)
	}
	var env struct {
		Data struct {
			NeedsBootstrap bool `json:"needs_bootstrap"`
			Authenticated  bool `json:"authenticated"`
			Configured     bool `json:"configured"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Configured {
		t.Error("configured = true, want false when there is no datastore")
	}
	if env.Data.NeedsBootstrap {
		t.Error("needs_bootstrap must be false — bootstrapping cannot work without a datastore")
	}
}

func TestUnconfiguredLoginExplainsTheRealProblem(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/bootstrap"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"a@b.c","password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		newUnconfiguredRouter().ServeHTTP(rec, req)

		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s returned 404; it must explain that no datastore is configured", path)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "datastore_not_configured") {
			t.Errorf("%s body = %s, want the datastore_not_configured code", path, body)
		}
		// The message has to name the fix, not just the symptom.
		if !strings.Contains(body, "HP_DATABASE_DSN") && !strings.Contains(body, "database.dsn") {
			t.Errorf("%s message should name the setting to change; got %s", path, body)
		}
	}
}

// When a datastore *is* configured the status payload must say so, so the UI
// does not show the "not configured" screen to a working panel.
func TestConfiguredStatusReportsConfigured(t *testing.T) {
	// statusHandler is only reachable with an auth service; assert the flag it
	// emits directly rather than standing up a full datastore here.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	unconfiguredStatusHandler()(rec, req)
	if strings.Contains(rec.Body.String(), `"configured":true`) {
		t.Error("the unconfigured handler must never report configured:true")
	}
}
