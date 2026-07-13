package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/thisisnkp/heropanel/internal/auth"
)

const (
	sessionCookieName = "hp_session"
	csrfCookieName    = "hp_csrf"
)

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

// setCSRFCookie issues a random double-submit CSRF token. It is readable by JS
// (not HttpOnly) so the SPA can echo it in the X-CSRF-Token header.
func setCSRFCookie(w http.ResponseWriter, maxAge int, secure bool) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(b),
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// isUnsafeMethod reports whether a method mutates state.
func isUnsafeMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// csrf enforces double-submit CSRF for cookie-authenticated mutations. It only
// applies when enabled and only when a session cookie is present (API-key /
// bearer requests carry no cookie and are exempt).
func csrf(enabled bool) mw {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if enabled && isUnsafeMethod(r.Method) {
				if _, err := r.Cookie(sessionCookieName); err == nil {
					token, terr := r.Cookie(csrfCookieName)
					header := r.Header.Get("X-CSRF-Token")
					if terr != nil || token.Value == "" || header == "" ||
						subtle.ConstantTimeCompare([]byte(header), []byte(token.Value)) != 1 {
						writeAPIError(w, r, http.StatusForbidden, "csrf_failed", "CSRF token missing or invalid.")
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// authenticate attaches a principal to the request context when a valid session
// or API key is presented. It never rejects: anonymous requests proceed (and are
// stopped later by requireAuth/requirePermission where required). A Bearer token
// beginning with "hp_" is treated as an API key; otherwise as a session token.
func authenticate(svc *auth.Service) mw {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tok := sessionToken(r); tok != "" {
				var (
					p   *auth.Principal
					err error
				)
				if strings.HasPrefix(tok, "hp_") {
					p, err = svc.AuthenticateAPIKey(r.Context(), tok)
				} else {
					p, err = svc.Authenticate(r.Context(), tok)
				}
				if err == nil {
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
