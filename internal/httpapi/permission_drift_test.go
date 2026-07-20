package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/auth"
)

// The permission each route is documented with (apiDocs) is hand-written
// metadata; the permission each route actually enforces lives in a
// requirePermission call in the router. Nothing tied the two together, so a
// route documented as `file.read` could be gated on `site.write` — or on
// nothing at all — and ship silently. The OpenAPI drift test only checks that a
// route *is* documented, not that the documentation is true.
//
// So this drives the real router: for every documented route, a principal with
// no permissions must be refused, and a principal holding exactly the documented
// permission must get past the gate. Together those pin the mapping in both
// directions.
//
// The services behind the router are stubs, so handlers past the gate fail with
// a 500 (or an "unavailable" from the service). That is fine, and deliberate:
// anything other than 401/403 means the request got *past* the permission gate,
// which is the only thing being asserted here.

// asPrincipal builds a request carrying a principal directly. The authenticate
// middleware only attaches a principal when a token is presented and never
// clears one, so a pre-seeded context survives the whole chain.
func asPrincipal(method, path string, permissions ...string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	p := &auth.Principal{UserID: 1, Kind: auth.KindUser, Permissions: permissions}
	return req.WithContext(auth.WithPrincipal(req.Context(), p))
}

// concreteURL turns a documented template into a requestable path. The values
// never resolve to anything — the gate runs before the handler looks them up.
func concreteURL(route string) string {
	r := strings.TrimPrefix(route, "/api/v1")
	for _, param := range []string{"{uid}", "{id}", "{name}", "{slug}", "{kind}", "{key}"} {
		r = strings.ReplaceAll(r, param, "x")
	}
	// Anything still templated is a param this test does not know; a literal
	// placeholder still routes, since chi matches any single segment.
	for strings.Contains(r, "{") {
		open := strings.Index(r, "{")
		close := strings.Index(r[open:], "}")
		if close < 0 {
			break
		}
		r = r[:open] + "x" + r[open+close+1:]
	}
	return "/api/v1" + r
}

// documentedPermissions returns every (method, route, permission) the API
// documents, taken from the live routing tree so a route that exists but is
// undocumented is caught by the OpenAPI drift test instead.
func documentedPermissions(t *testing.T, router http.Handler) map[string]string {
	t.Helper()
	out := map[string]string{}
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("router does not expose its routes")
	}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + strings.TrimSuffix(route, "/")
		meta, documented := apiDocs[key]
		if !documented || meta.Permission == "" {
			return nil
		}
		out[key] = meta.Permission
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no permission-gated routes were found; the walk or the doc keys must have changed")
	}
	return out
}

func TestEveryDocumentedPermissionActuallyGatesItsRoute(t *testing.T) {
	router := fullRouter(t).(http.Handler)

	for key, permission := range documentedPermissions(t, router) {
		method, route, _ := strings.Cut(key, " ")
		url := concreteURL(route)

		// A principal with nothing must never get through.
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, asPrincipal(method, url))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: a principal with no permissions got %d, want 403 — the route is documented as requiring %q but does not enforce it",
				key, rec.Code, permission)
			continue
		}

		// The documented permission must be the one that opens it. Anything other
		// than 401/403 means the gate let the request through to the handler.
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, asPrincipal(method, url, permission))
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
			t.Errorf("%s: holding the documented permission %q still got %d — the route enforces a different permission",
				key, permission, rec.Code)
		}
	}
}

// The wildcard is how an administrator is modelled, so it has to open every
// gated route. If it did not, an admin would hit a 403 with no way to grant
// themselves past it.
func TestWildcardPermissionOpensEveryGatedRoute(t *testing.T) {
	router := fullRouter(t).(http.Handler)

	for key := range documentedPermissions(t, router) {
		method, route, _ := strings.Cut(key, " ")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, asPrincipal(method, concreteURL(route), "*"))
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s: the wildcard permission was refused", key)
		}
	}
}

// An anonymous request must be told to authenticate, not told it lacks a
// permission — the two are different problems and lead to different fixes.
func TestGatedRoutesRejectAnonymousRequestsAsUnauthenticated(t *testing.T) {
	router := fullRouter(t).(http.Handler)

	for key := range documentedPermissions(t, router) {
		method, route, _ := strings.Cut(key, " ")
		req := httptest.NewRequest(method, concreteURL(route), strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: anonymous request got %d, want 401", key, rec.Code)
		}
	}
}
