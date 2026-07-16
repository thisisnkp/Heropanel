package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/runtime"
)

// getSiteRuntimeHandler returns a site's app runtime. Gated by "site.read".
func getSiteRuntimeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt, err := d.Runtime.GetRuntime(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, rt)
	}
}

// setSiteRuntimeHandler configures a site's app runtime (writes the systemd unit,
// starts it, and re-points the vhost as a reverse proxy). Gated by "site.write".
func setSiteRuntimeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Runtime string            `json:"runtime"`
			Command string            `json:"command"`
			Port    int               `json:"port"`
			Env     map[string]string `json:"env"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		rt, err := d.Runtime.SetRuntime(r.Context(), chi.URLParam(r, "uid"), runtime.SetInput{
			Runtime: req.Runtime, Command: req.Command, Port: req.Port, Env: req.Env,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, rt)
	}
}

// runtimeControlHandler starts, stops, or restarts a site's app unit. Gated by
// "site.write".
func runtimeControlHandler(d Deps, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt, err := d.Runtime.Control(r.Context(), chi.URLParam(r, "uid"), action)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, rt)
	}
}
