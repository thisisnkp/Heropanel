package httpapi

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func listDatabasesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Databases.ListDatabases(r.Context(), 0, 50, 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out == nil {
			out = []database.Instance{}
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

func createDatabaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "name", req.Name)
		out, err := d.Databases.CreateDatabase(r.Context(), p.UserID, req.Name)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "databases", out.UID)
		writeJSON(w, r, http.StatusCreated, out)
	}
}

func deleteDatabaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Databases.DeleteDatabase(r.Context(), chi.URLParam(r, "uid")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

func listDBUsersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Databases.ListUsers(r.Context(), 0, 50, 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out == nil {
			out = []database.User{}
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

func createDBUserHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Username string `json:"username"`
			Host     string `json:"host"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		// Username and host, never the password.
		audit.AddDetail(r.Context(), "username", req.Username)
		audit.AddDetail(r.Context(), "host", req.Host)
		out, err := d.Databases.CreateUser(r.Context(), p.UserID, req.Username, req.Host, req.Password)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "database-users", out.UID)
		writeJSON(w, r, http.StatusCreated, out)
	}
}

func grantDatabaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserUID    string   `json:"user_uid"`
			Privileges []string `json:"privileges"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "user_uid", req.UserUID)
		audit.AddDetail(r.Context(), "privileges", req.Privileges)
		if err := d.Databases.Grant(r.Context(), chi.URLParam(r, "uid"), req.UserUID, req.Privileges); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"granted": true})
	}
}

func revokeDatabaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserUID    string   `json:"user_uid"`
			Privileges []string `json:"privileges"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.AddDetail(r.Context(), "user_uid", req.UserUID)
		audit.AddDetail(r.Context(), "privileges", req.Privileges)
		if err := d.Databases.Revoke(r.Context(), chi.URLParam(r, "uid"), req.UserUID, req.Privileges); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"revoked": true})
	}
}

func deleteDBUserHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Databases.DeleteUser(r.Context(), chi.URLParam(r, "uid")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// adminerSSOHandler mints a throwaway database account and returns the fields
// the browser POSTs at Adminer's login form.
//
// The response carries a live password, so it is write-gated and marked
// no-store: nothing about it should sit in a cache or a history entry.
func adminerSSOHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Databases.StartSSO(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		// Handing someone a live database login is exactly the event a reviewer
		// needs to find later. Record which throwaway account was minted — the
		// account name is what ties this row to the queries the database's own
		// log will show. The password stays out of the chain.
		audit.AddDetail(r.Context(), "sso_username", out.Username)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, r, http.StatusCreated, out)
	}
}

func databaseSizeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.Databases.Size(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// exportDatabaseHandler dumps a database and streams the gzip back, then deletes
// the server-side copy. Synchronous on purpose: the client is waiting on the
// bytes, so there is nothing for a job + WebSocket round trip to report.
func exportDatabaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A GET that hands the caller every row in the database. The edge audits
		// mutations by default, and by that rule this would go unrecorded — which
		// would be absurd for the single most disclosing call in the API.
		audit.Force(r.Context())

		exp, err := d.Databases.Export(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "bytes", exp.Bytes)
		// A dump is a full copy of the customer's data. It goes away whether the
		// download succeeds, fails, or the client hangs up mid-stream.
		defer func() { _ = d.Databases.DiscardExport(exp.Path) }()

		f, err := os.Open(exp.Path)
		if err != nil {
			writeError(w, r, errx.Wrap(err, errx.KindInternal, "export_unreadable",
				"The export could not be read back."))
			return
		}
		defer func() { _ = f.Close() }()

		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+exp.File+`"`)
		if exp.Bytes > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(exp.Bytes, 10))
		}
		w.WriteHeader(http.StatusOK)
		// Errors past this point cannot become a status code — the header is
		// already out — so a broken pipe is simply the client going away.
		_, _ = io.Copy(w, f)
	}
}

// importDatabaseHandler loads an uploaded SQL dump into a database. The body is
// the dump itself (optionally gzipped), streamed to a staging file rather than
// buffered: an import is arbitrarily large, and so is the memory it would cost.
func importDatabaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gzipped := strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") ||
			strings.HasSuffix(strings.ToLower(r.URL.Query().Get("filename")), ".gz")
		path, file := d.Databases.ImportStagePath(gzipped)

		if err := os.MkdirAll(database.DumpDir, 0o700); err != nil {
			writeError(w, r, errx.Wrap(err, errx.KindInternal, "import_failed",
				"Could not prepare the upload directory."))
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			writeError(w, r, errx.Wrap(err, errx.KindInternal, "import_failed",
				"Could not stage the upload."))
			return
		}
		staged := true
		defer func() {
			if staged {
				_ = os.Remove(path) // the broker removes it on success
			}
		}()

		_, cerr := io.Copy(f, r.Body)
		if err := f.Close(); err != nil && cerr == nil {
			cerr = err
		}
		if cerr != nil {
			writeError(w, r, errx.Wrap(cerr, errx.KindInternal, "import_failed",
				"The upload could not be read."))
			return
		}

		if err := d.Databases.Import(r.Context(), chi.URLParam(r, "uid"), file); err != nil {
			writeError(w, r, err)
			return
		}
		staged = false
		writeJSON(w, r, http.StatusOK, map[string]any{"imported": true})
	}
}
