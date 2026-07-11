package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/thisisnkp/heropanel/internal/auth"
)

// decodeJSON decodes the request body into dst, writing a 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "bad_request", "Invalid JSON body.")
		return false
	}
	return true
}

// bootstrapHandler creates the first administrator (first-run flow). It succeeds
// only when no users exist yet.
func bootstrapHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		p, err := d.Auth.Bootstrap(r.Context(), req.Email, req.Username, req.Password)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, p)
	}
}

// loginHandler authenticates credentials and sets the session cookie.
func loginHandler(d Deps) http.HandlerFunc {
	secure := d.Config.Server.TLS.Enabled
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		token, p, err := d.Auth.Login(r.Context(), req.Email, req.Password, clientIP(r), r.UserAgent())
		if err != nil {
			writeError(w, r, err)
			return
		}
		setSessionCookie(w, token, d.Auth.SessionCookieMaxAge(), secure)
		writeJSON(w, r, http.StatusOK, p)
	}
}

// logoutHandler revokes the current session and clears the cookie.
func logoutHandler(d Deps) http.HandlerFunc {
	secure := d.Config.Server.TLS.Enabled
	return func(w http.ResponseWriter, r *http.Request) {
		if tok := sessionToken(r); tok != "" {
			_ = d.Auth.Logout(r.Context(), tok)
		}
		clearSessionCookie(w, secure)
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// meHandler returns the current principal (requireAuth guarantees presence).
func meHandler(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	writeJSON(w, r, http.StatusOK, p)
}
