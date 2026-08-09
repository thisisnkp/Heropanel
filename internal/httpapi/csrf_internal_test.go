package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	run := func(h http.Handler, method string, withSession, withCSRFCookie, withHeader bool, matching bool) int {
		req := httptest.NewRequest(method, "/api/v1/auth/logout", nil)
		if withSession {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess"})
		}
		if withCSRFCookie {
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok123"})
		}
		if withHeader {
			if matching {
				req.Header.Set("X-CSRF-Token", "tok123")
			} else {
				req.Header.Set("X-CSRF-Token", "wrong")
			}
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	enabled := csrf(true)(ok)
	disabled := csrf(false)(ok)

	// Safe method: always allowed.
	if c := run(enabled, http.MethodGet, true, true, false, false); c != http.StatusOK {
		t.Fatalf("GET should pass, got %d", c)
	}
	// Cookie-auth mutation without a CSRF header: rejected.
	if c := run(enabled, http.MethodPost, true, true, false, false); c != http.StatusForbidden {
		t.Fatalf("POST w/o header should be 403, got %d", c)
	}
	// Cookie-auth mutation with a mismatched header: rejected.
	if c := run(enabled, http.MethodPost, true, true, true, false); c != http.StatusForbidden {
		t.Fatalf("POST w/ mismatched token should be 403, got %d", c)
	}
	// Cookie-auth mutation with a matching header/cookie: allowed.
	if c := run(enabled, http.MethodPost, true, true, true, true); c != http.StatusOK {
		t.Fatalf("POST w/ matching token should pass, got %d", c)
	}
	// No session cookie (API-key/bearer): exempt.
	if c := run(enabled, http.MethodPost, false, false, false, false); c != http.StatusOK {
		t.Fatalf("bearer POST should be exempt, got %d", c)
	}
	// Disabled: never enforced.
	if c := run(disabled, http.MethodPost, true, false, false, false); c != http.StatusOK {
		t.Fatalf("disabled CSRF should pass, got %d", c)
	}
}
