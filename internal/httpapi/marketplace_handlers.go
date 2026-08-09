package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/nexpanel/internal/audit"
	"github.com/thisisnkp/nexpanel/internal/marketplace"
)

// Module marketplace edge. Browsing the catalog and the installed inventory is a
// read (module.read); installing, enabling, disabling, or removing a module is a
// host-wide act that runs third-party code (module.manage). Every mutation is
// audited against the module slug. Install is refused server-side for any module
// a trusted publisher key has not signed — the UI's disabled button is a
// courtesy, not the enforcement.

// listMarketplaceHandler returns the catalog with each entry's trust verdict and
// install state, plus whether a trust anchor is even configured. Gated by
// "module.read".
func listMarketplaceHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := d.Marketplace.Browse(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		if entries == nil {
			entries = []marketplace.CatalogEntry{}
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"modules":        entries,
			"trust_anchored": d.Marketplace.TrustAnchored(),
		})
	}
}

// listInstalledModulesHandler returns the operator's installed inventory. Gated
// by "module.read".
func listInstalledModulesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Marketplace.Installed(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"modules": list})
	}
}

// installModuleHandler verifies and installs a catalog module. Gated by
// "module.manage".
func installModuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		rec, err := d.Marketplace.Install(r.Context(), slug)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "modules", rec.Slug)
		audit.AddDetail(r.Context(), "version", rec.Version)
		audit.AddDetail(r.Context(), "publisher_key", rec.PublisherKey)
		writeJSON(w, r, http.StatusCreated, rec)
	}
}

// updateModuleHandler moves an installed module to the version the catalog now
// offers. Gated by "module.manage".
//
// The audit detail records the version it came *from* as well as the one it went
// to: after the fact, "which version was running when this happened" is the
// question, and the record alone would only answer the second half.
func updateModuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		rec, from, err := d.Marketplace.Update(r.Context(), slug)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "modules", rec.Slug)
		audit.AddDetail(r.Context(), "from_version", from)
		audit.AddDetail(r.Context(), "version", rec.Version)
		audit.AddDetail(r.Context(), "publisher_key", rec.PublisherKey)
		writeJSON(w, r, http.StatusOK, rec)
	}
}

// enableModuleHandler activates an installed module. Gated by "module.manage".
func enableModuleHandler(d Deps) http.HandlerFunc {
	return setModuleEnabled(d, true)
}

// disableModuleHandler parks an installed module. Gated by "module.manage".
func disableModuleHandler(d Deps) http.HandlerFunc {
	return setModuleEnabled(d, false)
}

func setModuleEnabled(d Deps, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		rec, err := d.Marketplace.SetEnabled(r.Context(), slug, enabled)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "modules", rec.Slug)
		audit.AddDetail(r.Context(), "state", rec.State)
		writeJSON(w, r, http.StatusOK, rec)
	}
}

// uninstallModuleHandler removes an installed module's record. Gated by
// "module.manage".
func uninstallModuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		if err := d.Marketplace.Uninstall(r.Context(), slug); err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "modules", slug)
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}
