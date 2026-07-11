package httpapi

import (
	"net/http"
	"strings"

	"github.com/thisisnkp/heropanel/internal/auth"
)

const sessionCookieName = "hp_session"

// sessionToken extracts the session token from the session cookie or, failing
// that, a Bearer Authorization header.
func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

func setSessionCookie(w http.ResponseWriter, token string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// authenticate attaches a principal to the request context when a valid session
// is presented. It never rejects: anonymous requests proceed (and are stopped
// later by requireAuth/requirePermission where required).
func authenticate(svc *auth.Service) mw {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tok := sessionToken(r); tok != "" {
				if p, err := svc.Authenticate(r.Context(), tok); err == nil {
					r = r.WithContext(auth.WithPrincipal(r.Context(), p))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireAuth rejects requests without an authenticated principal.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.FromContext(r.Context()); !ok {
			writeAPIError(w, r, http.StatusUnauthorized, "unauthenticated", "Authentication is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requirePermission rejects requests whose principal lacks the given permission.
func requirePermission(permission string) mw {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.FromContext(r.Context())
			if !ok {
				writeAPIError(w, r, http.StatusUnauthorized, "unauthenticated", "Authentication is required.")
				return
			}
			if !p.Can(permission) {
				writeAPIError(w, r, http.StatusForbidden, "forbidden", "You do not have permission to perform this action.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
