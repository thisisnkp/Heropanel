package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/job"
)

// jobView is the API representation of a job.
type jobView struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Status    string          `json:"status"`
	Progress  int             `json:"progress"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	WSChannel string          `json:"ws_channel"`
	CreatedAt string          `json:"created_at"`
}

func toJobView(j *job.Job) jobView {
	v := jobView{
		ID:        j.UID,
		Type:      j.Type,
		Status:    j.Status,
		Progress:  j.Progress,
		WSChannel: "job:" + j.UID,
		CreatedAt: j.CreatedAt,
	}
	if len(j.Result) > 0 && string(j.Result) != "{}" {
		v.Result = json.RawMessage(j.Result)
	}
	if j.Error.Valid {
		v.Error = j.Error.String
	}
	return v
}

// listJobsHandler returns the current user's jobs (admins see all). Requires auth.
func listJobsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		owner := p.UserID
		if p.Can("*") {
			owner = 0 // admins see all jobs
		}
		jobs, err := d.Jobs.List(r.Context(), owner, 50, 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out := make([]jobView, len(jobs))
		for i := range jobs {
			out[i] = toJobView(&jobs[i])
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// getJobHandler returns one job by id. Requires auth.
func getJobHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		j, err := d.Jobs.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, toJobView(j))
	}
}
