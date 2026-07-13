package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/database"
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
		out, err := d.Databases.CreateDatabase(r.Context(), p.UserID, req.Name)
		if err != nil {
			writeError(w, r, err)
			return
		}
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
		out, err := d.Databases.CreateUser(r.Context(), p.UserID, req.Username, req.Host, req.Password)
		if err != nil {
			writeError(w, r, err)
			return
		}
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
		if err := d.Databases.Grant(r.Context(), chi.URLParam(r, "uid"), req.UserUID, req.Privileges); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"granted": true})
	}
}
