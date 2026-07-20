package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Terminal session recordings.
//
// A recording is the transcript of the most powerful thing the panel hands out,
// so reading one is itself a privileged act: every route here is force-audited,
// including the reads. "Who watched whose session" is exactly the question an
// audit log exists to answer.

// listRecordingsHandler lists recordings, scoped to a site when the route
// carries one. Gated by "terminal.recordings.read".
func listRecordingsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var siteID int64
		if uid := chi.URLParam(r, "uid"); uid != "" {
			ref, err := d.Terminal.Site(r.Context(), uid)
			if err != nil {
				writeError(w, r, err)
				return
			}
			siteID = ref.ID
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

		recs, err := d.Recordings.List(r.Context(), siteID, limit, offset)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, recs)
	}
}

// getRecordingHandler returns one recording's metadata.
func getRecordingHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec, err := d.Recordings.Get(r.Context(), chi.URLParam(r, "rid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, rec)
	}
}

// downloadRecordingHandler streams the asciicast itself. Force-audited: this is
// the route that actually hands over the transcript.
func downloadRecordingHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rid := chi.URLParam(r, "rid")
		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "recording_uid", rid)

		body, rec, err := d.Recordings.Open(r.Context(), rid)
		if err != nil {
			writeError(w, r, err)
			return
		}
		defer func() { _ = body.Close() }()
		audit.AddDetail(r.Context(), "system_user", rec.SystemUser)

		// The asciicast media type, so a downloaded file is recognised by
		// asciinema and friends rather than guessed at.
		w.Header().Set("Content-Type", "application/x-asciicast")
		w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(rec.UID+".cast")+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		sw := &statusCapturingWriter{ResponseWriter: w}
		if _, err := io.Copy(sw, body); err != nil && !sw.wrote {
			writeError(w, r, errx.Wrap(err, errx.KindInternal, "recording_read_failed",
				"The recording could not be read."))
		}
	}
}

// deleteRecordingHandler removes a recording and its file. Gated by
// "terminal.recordings.delete", which is separate from read on purpose: deleting
// an audit artifact is the one action an operator under scrutiny would most want,
// and it should be grantable to fewer people than viewing.
func deleteRecordingHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rid := chi.URLParam(r, "rid")
		audit.AddDetail(r.Context(), "recording_uid", rid)
		if err := d.Recordings.Delete(r.Context(), rid); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}
