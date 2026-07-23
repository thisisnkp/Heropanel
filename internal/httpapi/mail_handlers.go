package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
)

// The mail HTTP edge. Mail is its own resource (mail.read / mail.write): a
// mail domain is not a site, and site permissions must not leak into
// mailboxes. Passwords are write-only — accepted, hashed, never returned.

// listMailDomainsHandler returns all mail domains. Gated by "mail.read".
func listMailDomainsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Mail.ListDomains(r.Context(), 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"domains": out, "available": d.Mail.Available(),
		})
	}
}

// createMailDomainHandler adds a mail domain (provisioning the host on first
// use, idempotently). Gated by "mail.write".
func createMailDomainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var in struct {
			Domain string `json:"domain"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "mail_domain", in.Domain)
		out, err := d.Mail.CreateDomain(r.Context(), p.UserID, in.Domain)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// getMailDomainHandler returns one domain with its mailboxes and aliases.
// Gated by "mail.read".
func getMailDomainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dom, accounts, aliases, err := d.Mail.GetDomain(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"domain": dom, "accounts": accounts, "aliases": aliases,
		})
	}
}

// deleteMailDomainHandler removes a mail domain. ?purge=true also deletes the
// stored mail — configuration removal and data destruction are separate acts.
// Gated by "mail.write".
func deleteMailDomainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		purge := r.URL.Query().Get("purge") == "true"
		audit.AddDetail(r.Context(), "mail_domain", uid)
		if purge {
			audit.AddDetail(r.Context(), "purge", "true")
		}
		if err := d.Mail.DeleteDomain(r.Context(), uid, purge); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "purged": purge})
	}
}

// checkMailDNSHandler resolves each of the domain's expected records (MX,
// SPF, DKIM, DMARC) against live DNS and reports what was found — the actual
// DNS, because the panel's own zone data would only prove the panel agrees
// with itself. Gated by "mail.read".
func checkMailDNSHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks, err := d.Mail.CheckDNS(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"records": checks})
	}
}

// createMailAccountHandler adds a mailbox. Gated by "mail.write".
func createMailAccountHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domainUID := chi.URLParam(r, "uid")
		var in struct {
			LocalPart string `json:"local_part"`
			Password  string `json:"password"`
			QuotaMB   int    `json:"quota_mb"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "mail_domain", domainUID)
		audit.AddDetail(r.Context(), "local_part", in.LocalPart)
		out, err := d.Mail.CreateAccount(r.Context(), domainUID, in.LocalPart, in.Password, in.QuotaMB)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// setMailAccountPasswordHandler replaces a mailbox password (write-only both
// directions). Gated by "mail.write".
func setMailAccountPasswordHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		var in struct {
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "mail_account", uid)
		if err := d.Mail.SetAccountPassword(r.Context(), uid, in.Password); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// setMailAccountQuotaHandler changes a mailbox quota. Gated by "mail.write".
func setMailAccountQuotaHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		var in struct {
			QuotaMB int `json:"quota_mb"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "mail_account", uid)
		if err := d.Mail.SetAccountQuota(r.Context(), uid, in.QuotaMB); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// setMailAccountStatusHandler suspends or reactivates a mailbox — suspension
// blocks logins but the mailbox keeps receiving. Gated by "mail.write".
func setMailAccountStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		var in struct {
			Status string `json:"status"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "mail_account", uid)
		audit.AddDetail(r.Context(), "status", in.Status)
		if err := d.Mail.SetAccountStatus(r.Context(), uid, in.Status); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// deleteMailAccountHandler removes a mailbox; ?purge=true also deletes its
// stored mail. Gated by "mail.write".
func deleteMailAccountHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domainUID, accountUID := chi.URLParam(r, "uid"), chi.URLParam(r, "aid")
		purge := r.URL.Query().Get("purge") == "true"
		audit.AddDetail(r.Context(), "mail_account", accountUID)
		if purge {
			audit.AddDetail(r.Context(), "purge", "true")
		}
		if err := d.Mail.DeleteAccount(r.Context(), domainUID, accountUID, purge); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "purged": purge})
	}
}

// createMailAliasHandler adds an alias/forwarder. Gated by "mail.write".
func createMailAliasHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domainUID := chi.URLParam(r, "uid")
		var in struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "mail_domain", domainUID)
		audit.AddDetail(r.Context(), "alias", in.Source+" -> "+in.Destination)
		out, err := d.Mail.CreateAlias(r.Context(), domainUID, in.Source, in.Destination)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// mailQueueHandler returns the current mail queue. Gated by "mail.read".
func mailQueueHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		msgs, running, err := d.Mail.QueueList(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"messages": msgs, "running": running})
	}
}

// flushMailQueueHandler retries everything deferred. Gated by "mail.write".
func flushMailQueueHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Mail.QueueFlush(r.Context()); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// deleteMailQueueHandler removes specific queued messages. Gated by
// "mail.write".
func deleteMailQueueHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "queue_ids", strings.Join(in.IDs, ","))
		n, err := d.Mail.QueueDelete(r.Context(), in.IDs)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "deleted": n})
	}
}

// mailDomainUsageHandler reports each mailbox's storage against its quota.
// Gated by "mail.read".
func mailDomainUsageHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usage, err := d.Mail.DomainUsage(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"usage": usage})
	}
}

// deleteMailAliasHandler removes one alias pair. Gated by "mail.write".
func deleteMailAliasHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domainUID, aliasUID := chi.URLParam(r, "uid"), chi.URLParam(r, "lid")
		audit.AddDetail(r.Context(), "mail_alias", aliasUID)
		if err := d.Mail.DeleteAlias(r.Context(), domainUID, aliasUID); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}
