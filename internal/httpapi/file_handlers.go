package httpapi

import (
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/files"
)

// The File Manager HTTP edge. Paths are always site-relative and travel in the
// `path` query parameter (list/content/delete) or the JSON body (mkdir/rename/
// chmod/extract); the broker clamps every one under the site root, so the edge
// passes them straight through. Content transfer is raw bytes, not JSON: an
// editor save or a binary upload has no business being base64'd through an
// envelope, and the service already chunks it under the wire cap.

// listFilesHandler lists one directory of a site (non-recursive). Gated by
// "file.read". The directory is the `path` query param, relative to the site
// home; empty means the site root.
func listFilesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		audit.AddDetail(r.Context(), "path", rel)
		out, err := d.Files.List(r.Context(), chi.URLParam(r, "uid"), rel)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// readFileHandler streams a single file's raw bytes back. Gated by "file.read".
// It is force-audited: a file's contents leaving the server is a disclosure
// worth a log line, the same reasoning as a database export.
func readFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		if rel == "" {
			writeError(w, r, files.ErrPathRequired)
			return
		}
		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "path", rel)

		// The bytes are streamed as they arrive from the broker, so the status and
		// headers must go out before the first chunk. A mid-stream broker error
		// therefore cannot become a status code — but a resolve/permission error
		// surfaces on the very first read, before anything is written, which is the
		// common failure and does get a clean error envelope.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(rel)+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		sw := &statusCapturingWriter{ResponseWriter: w}
		if _, err := d.Files.Download(r.Context(), chi.URLParam(r, "uid"), rel, sw); err != nil {
			if !sw.wrote {
				writeError(w, r, err)
			}
			// Once bytes are on the wire the header is committed; a truncated body
			// is the only signal we can give, and the access log records the error.
			return
		}
	}
}

// archiveFileHandler downloads a directory as a .zip. Gated by "file.read".
//
// The archive is built server-side and removed once the response is done, so
// "download this folder" does not leave a copy of the folder sitting in the
// folder — which is what happens when the only route to it is compress-then-
// download.
func archiveFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		audit.AddDetail(r.Context(), "path", rel)

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(files.ArchiveName(rel))+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		sw := &statusCapturingWriter{ResponseWriter: w}
		if _, err := d.Files.DownloadArchive(r.Context(), chi.URLParam(r, "uid"), rel, sw); err != nil {
			if !sw.wrote {
				writeError(w, r, err)
			}
			return
		}
	}
}

// writeFileHandler writes a file from the raw request body, truncating it first.
// Gated by "file.write". This is both the editor's save and a file upload: the
// body is the file's exact bytes and is streamed to the broker in chunks.
func writeFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		if rel == "" {
			writeError(w, r, files.ErrPathRequired)
			return
		}
		audit.AddDetail(r.Context(), "path", rel)
		n, err := d.Files.Upload(r.Context(), chi.URLParam(r, "uid"), rel, r.Body)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "bytes", n)
		writeJSON(w, r, http.StatusOK, map[string]any{"path": rel, "bytes": n})
	}
}

// deleteFileHandler removes a file or directory tree. Gated by "file.write".
func deleteFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		if rel == "" {
			writeError(w, r, files.ErrPathRequired)
			return
		}
		audit.AddDetail(r.Context(), "path", rel)
		if err := d.Files.Remove(r.Context(), chi.URLParam(r, "uid"), rel); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// mkdirFileHandler creates a directory (and parents). Gated by "file.write".
func mkdirFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "path", req.Path)
		if err := d.Files.Mkdir(r.Context(), chi.URLParam(r, "uid"), req.Path); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, map[string]any{"path": req.Path})
	}
}

// renameFileHandler moves/renames a path within the site. Gated by "file.write".
func renameFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "from", req.From)
		audit.AddDetail(r.Context(), "to", req.To)
		if err := d.Files.Rename(r.Context(), chi.URLParam(r, "uid"), req.From, req.To); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"from": req.From, "to": req.To})
	}
}

// copyFileHandler duplicates a path within the site, and moveFileHandler
// relocates one. They are the server side of copy/cut/paste; both refuse to
// overwrite unless the caller explicitly asks for the destination to be renamed.
// Gated by "file.write".
func copyFileHandler(d Deps) http.HandlerFunc { return transferHandler(d, false) }

func moveFileHandler(d Deps) http.HandlerFunc { return transferHandler(d, true) }

func transferHandler(d Deps, move bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			From       string `json:"from"`
			To         string `json:"to"`
			OnConflict string `json:"on_conflict"` // fail (default) | rename
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		policy := files.ConflictFail
		if req.OnConflict == string(files.ConflictRename) {
			policy = files.ConflictRename
		}
		audit.AddDetail(r.Context(), "from", req.From)
		audit.AddDetail(r.Context(), "to", req.To)

		op := d.Files.Copy
		if move {
			op = d.Files.Move
		}
		dest, err := op(r.Context(), chi.URLParam(r, "uid"), req.From, req.To, policy)
		if err != nil {
			writeError(w, r, err)
			return
		}
		// The destination is echoed because it may not be the one that was asked
		// for: with on_conflict=rename the server picks a free name, and the UI has
		// to know which one to select afterwards.
		audit.AddDetail(r.Context(), "destination", dest)
		writeJSON(w, r, http.StatusOK, map[string]any{"from": req.From, "to": dest})
	}
}

// chmodFileHandler changes a path's mode. Gated by "file.write".
func chmodFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "path", req.Path)
		audit.AddDetail(r.Context(), "mode", req.Mode)
		if err := d.Files.Chmod(r.Context(), chi.URLParam(r, "uid"), req.Path, req.Mode); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"path": req.Path, "mode": req.Mode})
	}
}

// extractFileHandler unpacks an archive within the site. Gated by "file.write".
func extractFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Archive string `json:"archive"`
			Dest    string `json:"dest"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "archive", req.Archive)
		audit.AddDetail(r.Context(), "dest", req.Dest)
		if err := d.Files.Extract(r.Context(), chi.URLParam(r, "uid"), req.Archive, req.Dest); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"archive": req.Archive, "dest": req.Dest})
	}
}

// compressFileHandler creates an archive from selected entries. Gated by
// "file.write" — it writes a new file into the site.
func compressFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Sources []string `json:"sources"`
			Archive string   `json:"archive"`
			Format  string   `json:"format"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "archive", req.Archive)
		audit.AddDetail(r.Context(), "items", len(req.Sources))
		if err := d.Files.Compress(r.Context(), chi.URLParam(r, "uid"), req.Sources, req.Archive, req.Format); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, map[string]any{"archive": req.Archive, "items": len(req.Sources)})
	}
}

// chownFileHandler resets ownership of a path to the site's own user. Gated by
// "file.write".
func chownFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "path", req.Path)
		if err := d.Files.FixOwnership(r.Context(), chi.URLParam(r, "uid"), req.Path); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"path": req.Path})
	}
}

// searchFilesHandler runs a recursive name/content search. Gated by "file.read".
func searchFilesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := d.Files.Search(r.Context(), chi.URLParam(r, "uid"), q.Get("path"), q.Get("q"), q.Get("mode"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// sanitizeFilename reduces a site-relative path to a bare, quote-safe basename
// for the Content-Disposition header, so a path can never break out of the
// quoted filename or inject header syntax.
func sanitizeFilename(rel string) string {
	base := path.Base(rel)
	if base == "." || base == "/" || base == "" {
		return "download"
	}
	base = strings.ReplaceAll(base, `"`, "")
	base = strings.ReplaceAll(base, "\\", "")
	base = strings.ReplaceAll(base, "\r", "")
	base = strings.ReplaceAll(base, "\n", "")
	return base
}

// statusCapturingWriter records whether any body byte has been written, so a
// streaming handler knows if it can still emit an error envelope.
type statusCapturingWriter struct {
	http.ResponseWriter
	wrote bool
}

func (s *statusCapturingWriter) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}
