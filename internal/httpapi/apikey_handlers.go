package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/auth"
)

// listAPIKeysHandler returns the current user's API keys (never the secrets).
func listAPIKeysHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		keys, err := d.Auth.ListAPIKeys(r.Context(), p.UserID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, keys)
	}
}

// createAPIKeyHandler creates a scoped API key and returns the plaintext once.
func createAPIKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Name   string   `json:"name"`
			Scopes []string `json:"scopes"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		key, view, err := d.Auth.CreateAPIKey(r.Context(), p.UserID, req.Name, req.Scopes)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, map[string]any{
			"key":     key, // shown once
			"api_key": view,
		})
	}
}

// revokeAPIKeyHandler revokes one of the current user's API keys.
func revokeAPIKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		if err := d.Auth.RevokeAPIKey(r.Context(), p.UserID, chi.URLParam(r, "uid")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}
