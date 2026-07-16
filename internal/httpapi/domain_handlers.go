package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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
		out, err := d.Domains.Add(r.Context(), chi.URLParam(r, "uid"), domain.AddInput{
			FQDN: req.FQDN, Kind: req.Kind, RedirectTo: req.RedirectTo, RedirectCode: req.RedirectCode,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
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
