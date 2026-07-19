package httpapi

import (
	"net/http"
	"strconv"

	"github.com/thisisnkp/heropanel/internal/audit"
)

const maxAuditPageSize = 200

func listAuditHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Reading who-did-what is itself worth recording: it is the first step of
		// working out what an operator knew, and the last step of an intruder
		// checking whether they were seen.
		audit.Force(r.Context())

		q := r.URL.Query()
		f := audit.Filter{
			ResourceType: q.Get("resource_type"),
			ResourceID:   q.Get("resource_id"),
			Action:       q.Get("action"),
			Limit:        atoiDefault(q.Get("limit"), 50),
			Offset:       atoiDefault(q.Get("offset"), 0),
		}
		if f.Limit > maxAuditPageSize {
			f.Limit = maxAuditPageSize
		}
		if f.Limit < 1 {
			f.Limit = 1 // never let a caller reach the repository's "-1 = all rows"
		}
		if f.Offset < 0 {
			f.Offset = 0
		}
		if v := q.Get("actor_user_id"); v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
				f.ActorUserID = id
			}
		}

		out, err := d.Audit.List(r.Context(), f)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out == nil {
			out = []audit.Entry{}
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// verifyAuditHandler walks the chain and reports the first break.
func verifyAuditHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		audit.Force(r.Context())

		if err := d.Audit.Verify(r.Context()); err != nil {
			// A broken chain is not a server error and must not be reported as
			// one: the request succeeded and the answer is "no". Returning 500
			// would hide the finding behind a generic failure and, worse, let it
			// be dismissed as a transient glitch.
			writeJSON(w, r, http.StatusOK, map[string]any{
				"intact": false,
				"error":  err.Error(),
			})
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"intact": true})
	}
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
