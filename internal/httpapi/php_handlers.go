package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/php"
)

// getSitePHPHandler returns a PHP site's pool configuration. Gated by "site.read".
func getSitePHPHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Sites.GetPHP(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// setSitePHPHandler replaces a PHP site's settings: version, memory limit, FPM
// sizing, allowlisted php.ini overrides, and OPcache. Gated by "site.write".
//
// A full replace, matching the service: every field is part of one envelope
// because all of it renders into one pool file.
func setSitePHPHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version       string            `json:"version"`
			MemoryLimitMB int               `json:"memory_limit_mb"`
			FPM           php.FPM           `json:"fpm"`
			INI           map[string]string `json:"ini"`
			OPcache       *php.OPcache      `json:"opcache"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		settings := php.Settings{
			Version:       req.Version,
			MemoryLimitMB: req.MemoryLimitMB,
			FPM:           req.FPM,
			INI:           req.INI,
			OPcache:       php.DefaultOPcache(),
		}
		// OPcache is a pointer so an omitted object means "the default" rather
		// than "disabled" — a bare `false` from an absent struct would silently
		// turn OPcache off on every settings save that did not mention it.
		if req.OPcache != nil {
			settings.OPcache = *req.OPcache
		}

		audit.AddDetail(r.Context(), "version", settings.Version)
		audit.AddDetail(r.Context(), "pm", settings.FPM.PM)
		audit.AddDetail(r.Context(), "pm_max_children", settings.FPM.MaxChildren)
		audit.AddDetail(r.Context(), "memory_limit_mb", settings.MemoryLimitMB)

		out, err := d.Sites.SetPHPSettings(r.Context(), chi.URLParam(r, "uid"), settings)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// listPHPExtensionsHandler reports a PHP version's extensions. Gated by
// "system.read": this is server state, not a site's.
func listPHPExtensionsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.PHP.ListExtensions(r.Context(), r.URL.Query().Get("version"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"version":   out.Version,
			"available": out.Available,
			"enabled":   out.Enabled,
			// Carried in the response so a client cannot show this list without
			// the caveat that comes with it.
			"scope_note": php.ExtensionScopeNote,
		})
	}
}

// setPHPExtensionHandler enables or disables an extension for a PHP version.
// Gated by "system.write" — it restarts PHP-FPM and affects every site on that
// version, which is emphatically not a per-site permission.
func setPHPExtensionHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version   string `json:"version"`
			Extension string `json:"extension"`
			Enabled   bool   `json:"enabled"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.SetResource(r.Context(), "php-extensions", req.Version)
		audit.AddDetail(r.Context(), "extension", req.Extension)
		audit.AddDetail(r.Context(), "enabled", req.Enabled)

		out, err := d.PHP.SetExtension(r.Context(), req.Version, req.Extension, req.Enabled)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"version":    out.Version,
			"available":  out.Available,
			"enabled":    out.Enabled,
			"scope_note": php.ExtensionScopeNote,
		})
	}
}
