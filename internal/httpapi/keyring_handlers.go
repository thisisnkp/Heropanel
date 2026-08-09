package httpapi

import (
	"net/http"

	"github.com/thisisnkp/nexpanel/internal/audit"
)

// keyringStatusHandler reports the rotating data-key envelope's state. Gated by
// "system.read".
func keyringStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.Keyring.Status(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, st)
	}
}

// rotateKeyringHandler mints a new active data key. New sealed values use it;
// existing values keep opening under their own generation. Gated by
// "system.write".
func rotateKeyringHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.Keyring.Rotate(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "active_generation", st.ActiveGeneration)
		writeJSON(w, r, http.StatusOK, st)
	}
}
