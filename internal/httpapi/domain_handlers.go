package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/domain"
)

// listDomainsHandler returns a site's domains. Gated by "site.read".
func listDomainsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Domains.List(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out == nil {
			out = []domain.Domain{}
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// addDomainHandler attaches an alias or redirect domain to a site and re-renders
// the vhost. Gated by "site.write".
func addDomainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			FQDN         string `json:"fqdn"`
			Kind         string `json:"kind"`
			RedirectTo   string `json:"redirect_to"`
			RedirectCode int    `json:"redirect_code"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "fqdn", req.FQDN)
		audit.AddDetail(r.Context(), "kind", req.Kind)
		out, err := d.Domains.Add(r.Context(), chi.URLParam(r, "uid"), domain.AddInput{
			FQDN: req.FQDN, Kind: req.Kind, RedirectTo: req.RedirectTo, RedirectCode: req.RedirectCode,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "domain_uid", out.UID)
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// deleteDomainHandler removes a domain from a site. Gated by "site.write".
func deleteDomainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Domains.Delete(r.Context(), chi.URLParam(r, "did")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// setForceHTTPSHandler toggles force-HTTPS for all of a site's domains. Gated by
// "site.write".
func setForceHTTPSHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := d.Domains.SetForceHTTPS(r.Context(), chi.URLParam(r, "uid"), req.Enabled); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"force_https": req.Enabled})
	}
}

// ── parked domains ───────────────────────────────────────────────────────────
//
// The account-level registry: a domain parked with no site yet, proven via a
// DNS TXT ownership challenge. See internal/domain/parked.go.

// listParkedDomainsHandler returns the caller's parked domains. Gated by
// "domain.read".
func listParkedDomainsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		out, err := d.Domains.ListParked(r.Context(), p.UserID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out == nil {
			out = []domain.ParkedDomain{}
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// parkDomainHandler registers ownership of a domain with no site attached and
// returns the DNS challenge instructions. Gated by "domain.write".
func parkDomainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			FQDN string `json:"fqdn"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "fqdn", req.FQDN)
		out, err := d.Domains.Park(r.Context(), p.UserID, req.FQDN)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "domain_uid", out.UID)
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// verifyParkedDomainHandler re-checks the live DNS TXT challenge. Gated by
// "domain.write".
func verifyParkedDomainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Domains.VerifyParked(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// deleteParkedDomainHandler unparks a domain. Refused (409) while it is
// attached to a site. Gated by "domain.write".
func deleteParkedDomainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Domains.DeleteParked(r.Context(), chi.URLParam(r, "uid")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// freeDomainsHandler lists domains available to pick when creating a site:
// verified parked domains and panel-hosted DNS zones not already attached to
// one. Gated by "domain.read".
func freeDomainsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		pool, err := d.Domains.DomainPool(r.Context(), p.UserID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		// Never null: the create-site form iterates both without guarding, and a
		// JSON null there is a crash rather than an empty picker.
		if pool.Free == nil {
			pool.Free = []string{}
		}
		if pool.Trusted == nil {
			pool.Trusted = []string{}
		}
		writeJSON(w, r, http.StatusOK, pool)
	}
}
