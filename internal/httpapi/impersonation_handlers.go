package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
)

// Audited impersonation: an administrator holding "user.impersonate" opens a
// short-lived session that acts *as* another user, with that user's exact
// permissions. Every mutation made while impersonating is attributed to the real
// admin in the audit log (see auditor), so accountability is never lost. The
// admin returns to their own identity with a single "stop" call.

// impersonateHandler starts an impersonation session for the target user and
// swaps the caller's session cookie to it. Gated by "user.impersonate".
func impersonateHandler(d Deps) http.HandlerFunc {
	secure := d.Config.Server.TLS.Enabled
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		// Impersonation must not nest: an already-impersonated session cannot open
		// another, or the impersonator chain (and thus accountability) would blur.
		if p.Impersonated() {
			writeAPIError(w, r, http.StatusConflict, "already_impersonating",
				"Stop the current impersonation before starting another.")
			return
		}
		targetUID := chi.URLParam(r, "uid")
		res, err := d.Auth.StartImpersonation(r.Context(), p.UserID, targetUID, clientIP(r), r.UserAgent())
		if err != nil {
			writeError(w, r, err)
			return
		}
		// Attribute the start to the acting admin (the edge already sees p.UserID),
		// naming who was impersonated.
		audit.AddDetail(r.Context(), "impersonated_user", res.Target.UserUID)
		audit.AddDetail(r.Context(), "impersonated_email", res.Target.Email)

		setSessionCookie(w, res.SessionToken, int(auth.ImpersonationTTL.Seconds()), secure)
		setCSRFCookie(w, int(auth.ImpersonationTTL.Seconds()), secure)
		writeJSON(w, r, http.StatusOK, res.Target)
	}
}

// stopImpersonationHandler ends the current impersonation and restores the
// admin's own session. Any authenticated (impersonated) caller may stop.
func stopImpersonationHandler(d Deps) http.HandlerFunc {
	secure := d.Config.Server.TLS.Enabled
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := d.Auth.StopImpersonation(r.Context(), sessionToken(r), clientIP(r), r.UserAgent())
		if err != nil {
			writeError(w, r, err)
			return
		}
		setSessionCookie(w, res.SessionToken, d.Auth.SessionCookieMaxAge(), secure)
		setCSRFCookie(w, d.Auth.SessionCookieMaxAge(), secure)
		writeJSON(w, r, http.StatusOK, res.Principal)
	}
}
