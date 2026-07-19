package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
)

// auditLoginActor files an authentication under the account it authenticated,
// rather than under "anonymous". The edge cannot infer this: authenticate() runs
// before the handler and finds no session, because minting the session is what
// the request is for.
func auditLoginActor(r *http.Request, p *auth.Principal) {
	if p == nil {
		return
	}
	audit.SetActor(r.Context(), p.UserID, audit.ActorUser)
	audit.SetResource(r.Context(), "users", p.UserUID)
}

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
		audit.AddDetail(r.Context(), "email", req.Email)
		p, err := d.Auth.Bootstrap(r.Context(), req.Email, req.Username, req.Password)
		if err != nil {
			writeError(w, r, err)
			return
		}
		// The very first entry in the chain: the creation of the account that
		// owns everything after it.
		auditLoginActor(r, p)
		writeJSON(w, r, http.StatusCreated, p)
	}
}

// statusHandler reports first-run state so the UI can choose between the login
// screen and the administrator bootstrap screen. It is public (no auth).
func statusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		needs, err := d.Auth.NeedsBootstrap(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		_, authed := auth.FromContext(r.Context())
		writeJSON(w, r, http.StatusOK, map[string]any{
			"needs_bootstrap": needs,
			"authenticated":   authed,
		})
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
		// Record the address the login was for, on success *and* failure: a run
		// of failures against one account is the signal that matters, and it is
		// invisible if only successful logins name their subject. The password is
		// never touched here — not even its length.
		audit.AddDetail(r.Context(), "email", req.Email)

		res, err := d.Auth.Login(r.Context(), req.Email, req.Password, clientIP(r), r.UserAgent())
		if err != nil {
			writeError(w, r, err)
			return
		}
		if res.MFARequired {
			audit.SetActor(r.Context(), res.Principal.UserID, audit.ActorUser)
			audit.AddDetail(r.Context(), "mfa_required", true)
			writeJSON(w, r, http.StatusOK, map[string]any{"mfa_required": true, "mfa_token": res.MFAToken})
			return
		}
		auditLoginActor(r, res.Principal)
		setSessionCookie(w, res.SessionToken, d.Auth.SessionCookieMaxAge(), secure)
		setCSRFCookie(w, d.Auth.SessionCookieMaxAge(), secure)
		writeJSON(w, r, http.StatusOK, res.Principal)
	}
}

// mfaCompleteHandler finishes an MFA login by verifying the TOTP code.
func mfaCompleteHandler(d Deps) http.HandlerFunc {
	secure := d.Config.Server.TLS.Enabled
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MFAToken string `json:"mfa_token"`
			Code     string `json:"code"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		res, err := d.Auth.CompleteMFA(r.Context(), req.MFAToken, req.Code, clientIP(r), r.UserAgent())
		if err != nil {
			writeError(w, r, err)
			return
		}
		auditLoginActor(r, res.Principal)
		setSessionCookie(w, res.SessionToken, d.Auth.SessionCookieMaxAge(), secure)
		setCSRFCookie(w, d.Auth.SessionCookieMaxAge(), secure)
		writeJSON(w, r, http.StatusOK, res.Principal)
	}
}

// mfaSetupHandler starts MFA enrollment, returning the secret + otpauth URI.
func mfaSetupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		secret, uri, err := d.Auth.SetupMFA(r.Context(), p.UserID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"secret": secret, "otpauth_uri": uri})
	}
}

// mfaEnableHandler enables MFA after verifying a code.
func mfaEnableHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Code string `json:"code"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := d.Auth.EnableMFA(r.Context(), p.UserID, req.Code); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"enabled": true})
	}
}

// mfaDisableHandler disables MFA after verifying a code.
func mfaDisableHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Code string `json:"code"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := d.Auth.DisableMFA(r.Context(), p.UserID, req.Code); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"enabled": false})
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
