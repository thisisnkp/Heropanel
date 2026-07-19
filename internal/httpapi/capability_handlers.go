package httpapi

import (
	"net/http"

	"github.com/thisisnkp/heropanel/internal/registry"
)

// capabilitiesHandler returns the flat set of capabilities currently available.
//
// This is what the UI gates on. It receives the set at login and renders or
// greys out features accordingly (docs/06 §6), so it never offers an action for
// a module that is not installed and then fails the click.
func capabilitiesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caps := []string{}
		if d.Registry != nil {
			caps = d.Registry.Capabilities()
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"capabilities": caps})
	}
}

// modulesHandler lists the registered modules and their states, for the module
// manager screen.
func modulesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mods := []registry.ModuleInfo{}
		if d.Registry != nil {
			mods = d.Registry.Modules()
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"modules": mods})
	}
}
