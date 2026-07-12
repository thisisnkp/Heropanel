package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/site"
)

// listSitesHandler returns sites. Gated by "site.read".
func listSitesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Admins (site.read via "*") see all; owner-scoping arrives with
		// reseller/client roles.
		sites, err := d.Sites.List(r.Context(), 0, 50, 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if sites == nil {
			sites = []site.Site{}
		}
		writeJSON(w, r, http.StatusOK, sites)
	}
}

// createSiteHandler provisions a new site. Gated by "site.write". When the async
// job queue is available it validates synchronously, enqueues a "site.create"
// job, and returns 202 + the job; otherwise it provisions synchronously (201).
func createSiteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Name          string `json:"name"`
			PrimaryDomain string `json:"primary_domain"`
			Type          string `json:"type"`
			DeployMode    string `json:"deploy_mode"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		in := site.CreateInput{
			Name:          req.Name,
			PrimaryDomain: req.PrimaryDomain,
			Type:          site.Type(req.Type),
			DeployMode:    site.DeployMode(req.DeployMode),
			OwnerID:       p.UserID,
		}

		if d.Jobs != nil {
			// Reject bad input up front, then enqueue.
			if err := site.ValidateInput(&in); err != nil {
				writeError(w, r, err)
				return
			}
			j, err := d.Jobs.Enqueue(r.Context(), "site.create", p.UserID, in)
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, r, http.StatusAccepted, map[string]any{"job": toJobView(j)})
			return
		}

		out, err := d.Sites.Create(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// getSiteHandler returns one site by UID. Gated by "site.read".
func getSiteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Sites.Get(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// deleteSiteHandler de-provisions a site. Gated by "site.write". Uses the async
// job queue when available (202 + job), otherwise deletes synchronously (200).
func deleteSiteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		uid := chi.URLParam(r, "uid")

		if d.Jobs != nil {
			j, err := d.Jobs.Enqueue(r.Context(), "site.delete", p.UserID, map[string]string{"uid": uid})
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, r, http.StatusAccepted, map[string]any{"job": toJobView(j)})
			return
		}

		if err := d.Sites.Delete(r.Context(), uid); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}
