package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
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

// setSitePHPHandler selects a PHP version for a PHP site. Gated by "site.write".
func setSitePHPHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Version string `json:"version"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		out, err := d.Sites.SetPHPVersion(r.Context(), chi.URLParam(r, "uid"), req.Version)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}
