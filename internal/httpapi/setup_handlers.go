package httpapi

import (
	"net/http"

	"github.com/thisisnkp/nexpanel/internal/audit"
	"github.com/thisisnkp/nexpanel/internal/setup"
)

// First-run setup wizard edge. The panel serves itself over net/http the moment
// the installer finishes, before any hosting stack exists; this is what the
// operator configures next. Both routes are reserved to administrators
// (setup.manage): the choices shape the whole host. The wizard's completion is
// also surfaced (unauthenticated-friendly) in /auth/status, so every client can
// gate the UI behind it without needing this admin-only detail.

// getSetupHandler returns the persisted setup state plus the selectable option
// catalogs, so the wizard can render exactly the choices the panel can honor
// (unsupported backends are marked, not hidden). Gated by "setup.manage".
func getSetupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := d.Setup.Status(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"state":      state,
			"webservers": setup.Webservers(),
			"db_engines": setup.DBEngines(),
		})
	}
}

// completeSetupHandler validates the operator's selection, provisions the host
// to match (when a provisioner is wired), and records the wizard as finished.
// Gated by "setup.manage".
func completeSetupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var sel setup.Selection
		if !decodeJSON(w, r, &sel) {
			return
		}
		state, err := d.Setup.Complete(r.Context(), sel)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "setup", "panel")
		audit.AddDetail(r.Context(), "webserver", string(sel.Webserver))
		audit.AddDetail(r.Context(), "db_engine", string(sel.DBEngine))
		audit.AddDetail(r.Context(), "manage_dns", boolStr(sel.ManageDNS))
		audit.AddDetail(r.Context(), "create_mail", boolStr(sel.CreateMail))
		writeJSON(w, r, http.StatusOK, state)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
