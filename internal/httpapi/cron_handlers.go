package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/cron"
)

// The scheduler HTTP edge. Jobs are site-scoped, so they ride on the site
// permissions: listing and reading logs is site.read; creating, toggling,
// running and deleting is site.write — the same grants that let someone change
// the site the jobs run inside.

// listCronJobsHandler returns a site's scheduled jobs. Gated by "site.read".
func listCronJobsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobs, err := d.Cron.List(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, jobs)
	}
}

// createCronJobHandler schedules a job. Gated by "site.write".
func createCronJobHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID := chi.URLParam(r, "uid")
		var in cron.CreateInput
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "site", siteUID)
		audit.AddDetail(r.Context(), "schedule", in.Schedule)
		job, err := d.Cron.Create(r.Context(), siteUID, in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, job)
	}
}

// toggleCronJobHandler enables or disables a job. Gated by "site.write".
func toggleCronJobHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID, jobUID := chi.URLParam(r, "uid"), chi.URLParam(r, "jid")
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		audit.AddDetail(r.Context(), "job", jobUID)
		if err := d.Cron.SetEnabled(r.Context(), siteUID, jobUID, body.Enabled); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "uid": jobUID, "enabled": body.Enabled})
	}
}

// runCronJobHandler triggers a job immediately. Gated by "site.write".
func runCronJobHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID, jobUID := chi.URLParam(r, "uid"), chi.URLParam(r, "jid")
		audit.AddDetail(r.Context(), "job", jobUID)
		if err := d.Cron.Run(r.Context(), siteUID, jobUID); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "uid": jobUID, "ran": true})
	}
}

// cronJobLogsHandler returns a job's captured output. Gated by "site.read" and
// force-audited — a job's output can carry data worth the same log line a file
// read gets.
func cronJobLogsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID, jobUID := chi.URLParam(r, "uid"), chi.URLParam(r, "jid")
		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "job", jobUID)
		log, err := d.Cron.Logs(r.Context(), siteUID, jobUID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"log": log})
	}
}

// deleteCronJobHandler removes a job. Gated by "site.write".
func deleteCronJobHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID, jobUID := chi.URLParam(r, "uid"), chi.URLParam(r, "jid")
		audit.AddDetail(r.Context(), "job", jobUID)
		if err := d.Cron.Delete(r.Context(), siteUID, jobUID); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "uid": jobUID})
	}
}
