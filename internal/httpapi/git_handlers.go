package httpapi

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
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
			AuthKind     string `json:"auth_kind"`
			AuthUsername string `json:"auth_username"`
			// Token is write-only: it is accepted here and never echoed back by
			// any response. Leave it out on an update to keep the stored one.
			Token     string `json:"token"`
			RotateKey bool   `json:"rotate_key"`
			// HostKey pins the repo host's SSH host key(s) (ssh_key auth). Public.
			HostKey string `json:"host_key"`
			// Pointer so "absent" and "false" stay distinguishable: omitting the
			// field keeps the stored setting instead of disabling Composer.
			AutoComposer *bool `json:"auto_composer"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		// The repo, branch and auth *kind* are the facts worth keeping. The token
		// and the deploy key are not: this row outlives the credential and is
		// meant to be exported.
		audit.AddDetail(r.Context(), "repo_url", req.RepoURL)
		audit.AddDetail(r.Context(), "branch", req.Branch)
		audit.AddDetail(r.Context(), "auth_kind", req.AuthKind)
		audit.AddDetail(r.Context(), "rotate_key", req.RotateKey)

		src, err := d.Git.SetSource(r.Context(), chi.URLParam(r, "uid"), git.SetSourceInput{
			RepoURL:      req.RepoURL,
			Branch:       req.Branch,
			BuildCommand: req.BuildCommand,
			WebRoot:      req.WebRoot,
			AuthKind:     req.AuthKind,
			AuthUsername: req.AuthUsername,
			Token:        req.Token,
			RotateKey:    req.RotateKey,
			HostKey:      req.HostKey,
			AutoComposer: req.AutoComposer,
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
		audit.AddDetail(r.Context(), "trigger", git.TriggerManual)
		if d.Jobs != nil {
			j, err := d.Jobs.Enqueue(r.Context(), "git.deploy", p.UserID,
				deployPayload{SiteUID: uid, Trigger: git.TriggerManual})
			if err != nil {
				writeError(w, r, err)
				return
			}
			audit.AddDetail(r.Context(), "job", j.UID)
			writeJSON(w, r, http.StatusAccepted, map[string]any{"job": toJobView(j)})
			return
		}
		out, err := d.Git.RunDeploy(r.Context(), uid, git.TriggerManual, job.Noop)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "deployment", out.UID)
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
		audit.AddDetail(r.Context(), "target_deployment", depUID)
		if d.Jobs != nil {
			j, err := d.Jobs.Enqueue(r.Context(), "git.rollback", p.UserID,
				rollbackPayload{SiteUID: uid, DeploymentUID: depUID})
			if err != nil {
				writeError(w, r, err)
				return
			}
			audit.AddDetail(r.Context(), "job", j.UID)
			writeJSON(w, r, http.StatusAccepted, map[string]any{"job": toJobView(j)})
			return
		}
		out, err := d.Git.RunRollback(r.Context(), uid, depUID, job.Noop)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "deployment", out.UID)
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// gitWebhookHandler is the push endpoint. It is unauthenticated by session and
// authorized solely by the per-source webhook secret (constant-time compare),
// then enqueues a deploy. Mounted outside the authenticated group.
func gitWebhookHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		// Read the body up front: GitHub's HMAC signature is computed over it, so
		// verification needs the exact bytes. The body is already bounded by the
		// bodyLimit middleware on the root router.
		body, _ := io.ReadAll(r.Body)
		secret := r.URL.Query().Get("secret")
		if secret == "" {
			secret = r.Header.Get("X-HeroPanel-Secret")
		}
		proof := git.WebhookProof{
			Body:        body,
			GitHubSig:   r.Header.Get("X-Hub-Signature-256"),
			GitLabToken: r.Header.Get("X-Gitlab-Token"),
			Secret:      secret,
		}
		// There is no principal here by design, so the entry stays "anonymous"
		// and the source's own uid is the only identity on record. A wrong proof
		// lands as outcome=denied, which is what makes a brute-force against the
		// webhook visible at all. We record which *kind* of proof was presented,
		// never its value.
		audit.AddDetail(r.Context(), "trigger", git.TriggerWebhook)
		audit.AddDetail(r.Context(), "webhook_auth", proof.Kind())
		if _, err := d.Git.VerifyWebhookSigned(r.Context(), uid, proof); err != nil {
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
			audit.AddDetail(r.Context(), "job", j.UID)
			writeJSON(w, r, http.StatusAccepted, map[string]any{"job": toJobView(j)})
			return
		}
		out, err := d.Git.RunDeploy(r.Context(), uid, git.TriggerWebhook, job.Noop)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "deployment", out.UID)
		writeJSON(w, r, http.StatusCreated, out)
	}
}
