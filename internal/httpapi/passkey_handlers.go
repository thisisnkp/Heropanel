package httpapi

import (
	"encoding/base64"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Passkeys (WebAuthn). Registration is done while signed in; login is
// passwordless. Binary fields cross the wire as base64url strings.

var b64url = base64.RawURLEncoding

// passkeyRegisterBeginHandler returns creation options for the signed-in user.
func passkeyRegisterBeginHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		opts, err := d.Auth.BeginPasskeyRegistration(r.Context(), p.UserID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, opts)
	}
}

// passkeyRegisterFinishHandler verifies the attestation and stores the passkey.
func passkeyRegisterFinishHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Name              string `json:"name"`
			ID                string `json:"id"`                 // base64url credential id
			ClientDataJSON    string `json:"client_data_json"`   // base64url
			AttestationObject string `json:"attestation_object"` // base64url
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		credID, cdj, att, ok := decodeB64URL(req.ID, req.ClientDataJSON, req.AttestationObject)
		if !ok {
			writeError(w, r, errx.Validation("bad_encoding", "Malformed passkey data."))
			return
		}
		audit.AddDetail(r.Context(), "passkey", req.Name)
		pk, err := d.Auth.FinishPasskeyRegistration(r.Context(), p.UserID, req.Name, credID, cdj, att)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, pk)
	}
}

// listPasskeysHandler returns the signed-in user's passkeys.
func listPasskeysHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		keys, err := d.Auth.ListPasskeys(r.Context(), p.UserID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"passkeys": keys, "enabled": d.Auth.PasskeysEnabled()})
	}
}

// deletePasskeyHandler removes one of the user's passkeys.
func deletePasskeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		uid := chi.URLParam(r, "uid")
		audit.AddDetail(r.Context(), "passkey", uid)
		if err := d.Auth.DeletePasskey(r.Context(), p.UserID, uid); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// passkeyLoginBeginHandler returns assertion options + an opaque login token.
// Unauthenticated.
func passkeyLoginBeginHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		opts, token, err := d.Auth.BeginPasskeyLogin(r.Context(), req.Email)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"options": opts, "login_token": token})
	}
}

// passkeyLoginFinishHandler verifies the assertion and issues a session.
// Unauthenticated.
func passkeyLoginFinishHandler(d Deps) http.HandlerFunc {
	secure := d.Config.Server.TLS.Enabled
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			LoginToken        string `json:"login_token"`
			ID                string `json:"id"`
			ClientDataJSON    string `json:"client_data_json"`
			AuthenticatorData string `json:"authenticator_data"`
			Signature         string `json:"signature"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		credID, cdj, authData, ok := decodeB64URL(req.ID, req.ClientDataJSON, req.AuthenticatorData)
		sig, sigErr := b64url.DecodeString(req.Signature)
		if !ok || sigErr != nil {
			writeError(w, r, errx.Validation("bad_encoding", "Malformed passkey data."))
			return
		}
		res, err := d.Auth.FinishPasskeyLogin(r.Context(), req.LoginToken, credID, cdj, authData, sig, clientIP(r), r.UserAgent())
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

// decodeB64URL decodes three base64url fields, reporting whether all succeeded.
func decodeB64URL(a, b, c string) ([]byte, []byte, []byte, bool) {
	da, ea := b64url.DecodeString(a)
	db, eb := b64url.DecodeString(b)
	dc, ec := b64url.DecodeString(c)
	return da, db, dc, ea == nil && eb == nil && ec == nil
}
