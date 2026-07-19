package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/site"
)

// getSiteLimitsHandler returns a site's resource limits. Gated by "site.read".
func getSiteLimitsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Sites.GetLimits(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// setSiteLimitsHandler applies a site's resource limits to its cgroup slice.
// Gated by "site.write".
func setSiteLimitsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A full replace, not a patch: every field is part of one envelope, and an
		// omitted field means "unlimited" rather than "leave as-is". Anything else
		// would make it ambiguous whether a limit was cleared or just not sent.
		var req struct {
			CPUQuotaPct   int   `json:"cpu_quota_pct"`
			MemLimitBytes int64 `json:"mem_limit_bytes"`
			PidsMax       int   `json:"pids_max"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		out, err := d.Sites.SetLimits(r.Context(), chi.URLParam(r, "uid"), site.Limits{
			CPUQuotaPct:   req.CPUQuotaPct,
			MemLimitBytes: req.MemLimitBytes,
			PidsMax:       req.PidsMax,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}
