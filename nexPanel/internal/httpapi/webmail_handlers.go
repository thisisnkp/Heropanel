package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/nexpanel/internal/audit"
)

// The webmail HTTP edge. Webmail rides the mail permission pair (mail.read /
// mail.write): it is the browser face of the same mailboxes, so a principal who
// can manage mail can turn webmail on and see its status. There are no mailbox
// passwords here — Roundcube authenticates each user against Dovecot at login.

// webmailStatusHandler reports whether webmail is enabled/installed and its URL.
// Gated by "mail.read".
func webmailStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.Webmail.Status(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, st)
	}
}

// installWebmailHandler lays down Roundcube's runtime and starts serving it.
// Gated by "mail.write".
func installWebmailHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.Webmail.Install(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "webmail_hostname", st.Hostname)
		writeJSON(w, r, http.StatusOK, st)
	}
}

// webmailSSOHandler mints a one-time Dovecot master credential for the mailbox
// and returns the hand-off the browser POSTs at Roundcube's login form — a
// passwordless sign-on that never exposes the mailbox's own password. Gated by
// "mail.write" (it creates a credential).
func webmailSSOHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		ho, err := d.Mail.StartWebmailSSO(r.Context(), uid)
		if err != nil {
			writeError(w, r, err)
			return
		}
		// The mailbox and expiry are safe to audit; the one-time password is not.
		audit.AddDetail(r.Context(), "mailbox", ho.User)
		writeJSON(w, r, http.StatusOK, ho)
	}
}
