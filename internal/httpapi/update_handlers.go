package httpapi

import (
	"net/http"

	"github.com/thisisnkp/nexpanel/internal/audit"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Panel self-update (docs/26). Reading is system.read; starting one is
// system.write and force-audited — replacing the panel's own binaries, the
// broker included, is the most consequential thing an operator can do from this
// API, and the audit row is the only durable record of who asked, since the
// process that served the request is destroyed by it.

// getUpdateHandler reports the running version, the channel, and what that
// channel currently offers. It never fails on an unreachable release server:
// the panel is not broken because releases.example.com is down, so the reason
// travels in the body instead of as a 502.
func getUpdateHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Update == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "update_unavailable",
				"Self-update is unavailable because the panel has no datastore."))
			return
		}
		writeJSON(w, r, http.StatusOK, d.Update.Status(r.Context()))
	}
}

// listUpdatesHandler returns recent update attempts, newest first. This is the
// only place a rolled-back update is visible after the fact — the operator who
// pressed the button was disconnected by the restart.
func listUpdatesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Update == nil {
			writeJSON(w, r, http.StatusOK, []any{})
			return
		}
		rows, err := d.Update.History(r.Context(), 20)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, rows)
	}
}

// checkUpdateHandler forces a manifest fetch rather than serving whatever the
// last look found.
func checkUpdateHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Update == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "update_unavailable",
				"Self-update is unavailable because the panel has no datastore."))
			return
		}
		st, err := d.Update.Check(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, st)
	}
}

// applyUpdateHandler downloads, verifies and starts an update.
//
// It answers 202, not 200, and that is the honest code: the work has been
// accepted and handed to a process outside this one. This handler cannot report
// the result — the installer restarts npd underneath it — so the response says
// "started", and the panel that comes back up reconciles the outcome.
func applyUpdateHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Update == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "update_unavailable",
				"Self-update is unavailable because the panel has no datastore."))
			return
		}
		var req struct {
			Version string `json:"version"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		// An empty version means "whatever the channel currently offers", so an
		// operator clicking Update does not have to retype a version they were
		// just shown.
		version := req.Version
		if version == "" {
			st, err := d.Update.Check(r.Context())
			if err != nil {
				writeError(w, r, err)
				return
			}
			if st.Available == "" {
				writeError(w, r, errx.Conflict("already_current",
					"There is no newer release on this channel."))
				return
			}
			version = st.Available
		}

		audit.SetResource(r.Context(), "panel", "update")
		audit.AddDetail(r.Context(), "from_version", d.Update.Version())
		audit.AddDetail(r.Context(), "to_version", version)

		row, err := d.Update.Apply(r.Context(), version)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusAccepted, map[string]any{
			"started": true,
			"uid":     row.UID,
			"from":    row.FromVersion,
			"to":      row.ToVersion,
			"note":    "The panel is restarting to apply the update. It will be briefly unavailable.",
		})
	}
}
