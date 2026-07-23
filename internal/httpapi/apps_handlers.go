package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/apps"
	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/docker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The one-click Apps HTTP edge.
//
// Apps are labelled compose stacks, so they reuse the Docker module's ownership
// boundary and add no privilege of their own. The permissions mirror that:
// browsing the catalog and reading an app's status/logs is `docker.read`;
// deploying and removing is `docker.write`. There is no separate "apps"
// permission, because an app is not a separate kind of privilege — it is a
// container stack, and whoever may create a container may deploy one.

// listAppTemplatesHandler returns the catalog with a memory-feasibility verdict
// on each. Gated by "docker.read".
func listAppTemplatesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, d.Apps.Catalog())
	}
}

// deployAppHandler validates, generates secrets, checks memory and brings a
// stack up. Gated by "docker.write".
//
// The generated secrets come back in the response and are **not** audited: they
// are shown to the operator once, and copying a freshly minted password into the
// audit log would defeat the point of generating it.
func deployAppHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in apps.DeployInput
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "template", in.Slug)
		audit.AddDetail(r.Context(), "app", in.Name)

		res, err := d.Apps.Deploy(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, res)
	}
}

// appStatusHandler lists a deployed app's services. Gated by "docker.read".
func appStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Apps.Status(r.Context(), chi.URLParam(r, "project"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// appLogsHandler returns a deployed app's combined logs. Gated by "docker.read"
// and force-audited, like container logs: an app's logs carry its secrets and
// its users' data.
func appLogsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project := chi.URLParam(r, "project")
		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "app", project)
		out, err := d.Apps.Logs(r.Context(), project, docker.ClampTail(0))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// exposeAppHandler fronts a deployed app with a domain by creating a proxy site
// whose vhost reverse-proxies to the app's live loopback port. Gated by
// "site.write": exposing an app to the internet is a site operation — it creates
// a real site, with the domain, TLS and suspend controls every site has — not
// merely a container tweak, so it is held to the site permission rather than
// docker.write.
//
// The app is fronted, not moved: it keeps running on loopback exactly as before,
// and the new proxy site is what the world reaches it through. Everything after —
// aliases, force-HTTPS, a certificate, suspension — is then managed on the Sites
// page like any other site, because it *is* one.
func exposeAppHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Sites == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "sites_unavailable",
				"Site management is not available on this host."))
			return
		}
		project := chi.URLParam(r, "project")
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Domain string `json:"domain"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "app", project)
		audit.AddDetail(r.Context(), "domain", req.Domain)

		// The app must actually be deployed before it can be fronted, or the proxy
		// site would point at a port nothing is listening on.
		services, err := d.Apps.Status(r.Context(), project)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if len(services) == 0 {
			writeError(w, r, errx.NotFound("app_not_deployed",
				"That app is not deployed, so there is nothing to expose."))
			return
		}

		s, err := d.Sites.ExposeApp(r.Context(), p.UserID, project, req.Domain)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, s)
	}
}

// unexposeAppHandler removes the proxy site fronting an app, dropping its vhost.
// Gated by "site.write" — it deletes a site. The app itself is left running on
// loopback; unexposing takes down the front door, not the app behind it.
func unexposeAppHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Sites == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "sites_unavailable",
				"Site management is not available on this host."))
			return
		}
		project := chi.URLParam(r, "project")
		audit.AddDetail(r.Context(), "app", project)
		if err := d.Sites.UnexposeApp(r.Context(), project); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "project": project})
	}
}

// appExposureHandler reports whether an app is exposed and at which domain, so
// the UI can show its address and offer unexpose. Gated by "docker.read".
func appExposureHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Sites == nil {
			writeJSON(w, r, http.StatusOK, map[string]any{"exposed": false})
			return
		}
		s, err := d.Sites.AppExposure(r.Context(), chi.URLParam(r, "project"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		if s == nil {
			writeJSON(w, r, http.StatusOK, map[string]any{"exposed": false})
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"exposed": true, "domain": s.PrimaryDomain, "site_uid": s.UID, "status": s.Status,
		})
	}
}

// removeAppHandler tears an app's stack down. Gated by "docker.write". The
// broker refuses a stack the panel did not create, and never removes volumes —
// so an app's data survives a tear-down and can be reattached by redeploying.
func removeAppHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project := chi.URLParam(r, "project")
		audit.AddDetail(r.Context(), "app", project)
		if err := d.Apps.Remove(r.Context(), project); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "project": project})
	}
}
