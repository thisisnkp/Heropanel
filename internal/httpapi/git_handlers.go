package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/job"
)

// deployPayload is the async job body for git.deploy.
type deployPayload struct {
	SiteUID string `json:"site_uid"`
	Trigger string `json:"trigger"`
}

// rollbackPayload is the async job body for git.rollback.
type rollbackPayload struct {
	SiteUID       string `json:"site_uid"`
	DeploymentUID string `json:"deployment_uid"`
}

// getSiteGitHandler returns a site's Git source. Gated by "git.read".
func getSiteGitHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		src, err := d.Git.GetSource(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, src)
	}
}

// setSiteGitHandler configures (or replaces) a site's Git source. Gated by
// "git.write". Returns the source including the one-time webhook URL.
func setSiteGitHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RepoURL      string `json:"repo_url"`
			Branch       string `json:"branch"`
			BuildCommand string `json:"build_command"`
			WebRoot      string `json:"web_root"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		src, err := d.Git.SetSource(r.Context(), chi.URLParam(r, "uid"), git.SetSourceInput{
			RepoURL:      req.RepoURL,
			Branch:       req.Branch,
			BuildCommand: req.BuildCommand,
			WebRoot:      req.WebRoot,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, src)
	}
}

// listSiteDeploymentsHandler returns a site's deploy history. Gated by "git.read".
func listSiteDeploymentsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deps, err := d.Git.ListDeployments(r.Context(), chi.URLParam(r, "uid"), 20)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if deps == nil {
			deps = []git.Deployment{}
		}
		writeJSON(w, r, http.StatusOK, deps)
	}
}

// deploySiteHandler triggers a deployment. Gated by "git.write". Enqueues an
// async job when the queue is available (202 + job), else deploys synchronously.
func deploySiteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		uid := chi.URLParam(r, "uid")
		if d.Jobs != nil {
			j, err := d.Jobs.Enqueue(r.Context(), "git.deploy", p.UserID,
				deployPayload{SiteUID: uid, Trigger: git.TriggerManual})
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, r, http.StatusAccepted, map[string]any{"job": toJobView(j)})
			return
		}
		out, err := d.Git.RunDeploy(r.Context(), uid, git.TriggerManual, job.Noop)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// rollbackSiteHandler rolls a site back to a prior deployment. Gated by
// "git.write". Async when the queue is available (202 + job).
func rollbackSiteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		uid := chi.URLParam(r, "uid")
		depUID := chi.URLParam(r, "dep")
		if d.Jobs != nil {
			j, err := d.Jobs.Enqueue(r.Context(), "git.rollback", p.UserID,
				rollbackPayload{SiteUID: uid, DeploymentUID: depUID})
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, r, http.StatusAccepted, map[string]any{"job": toJobView(j)})
			return
		}
		out, err := d.Git.RunRollback(r.Context(), uid, depUID, job.Noop)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// gitWebhookHandler is the push endpoint. It is unauthenticated by session and
// authorized solely by the per-source webhook secret (constant-time compare),
// then enqueues a deploy. Mounted outside the authenticated group.
func gitWebhookHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		secret := r.URL.Query().Get("secret")
		if secret == "" {
			secret = r.Header.Get("X-HeroPanel-Secret")
		}
		if _, err := d.Git.VerifyWebhook(r.Context(), uid, secret); err != nil {
			writeError(w, r, err)
			return
		}
		if d.Jobs != nil {
			j, err := d.Jobs.Enqueue(r.Context(), "git.deploy", 0,
				deployPayload{SiteUID: uid, Trigger: git.TriggerWebhook})
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, r, http.StatusAccepted, map[string]any{"job": toJobView(j)})
			return
		}
		out, err := d.Git.RunDeploy(r.Context(), uid, git.TriggerWebhook, job.Noop)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}
