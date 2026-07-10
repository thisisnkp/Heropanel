package httpapi

import (
	"net/http"
	"runtime"
	"time"
)

// healthHandler is a liveness probe: 200 while the process is up. It uses a
// minimal body (not the API envelope) so external probes stay simple.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// readyHandler is a readiness probe. Dependency checks (database, redis, broker)
// are added as those components are wired; for now they report "skipped".
func readyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		components := map[string]string{
			"database": "skipped",
			"redis":    "skipped",
			"broker":   "skipped",
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"status":     "ready",
			"components": components,
		})
	}
}

// systemInfoHandler reports build/runtime information (docs/04: /system/info).
// It will be moved behind auth when the auth layer lands.
func systemInfoHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]any{
			"product":        "HeroPanel",
			"version":        d.Version,
			"go":             runtime.Version(),
			"os":             runtime.GOOS,
			"arch":           runtime.GOARCH,
			"cpus":           runtime.NumCPU(),
			"started_at":     d.StartedAt.UTC().Format(time.RFC3339),
			"uptime_seconds": int(time.Since(d.StartedAt).Seconds()),
		})
	}
}

// rootHandler serves a placeholder page where the embedded React SPA will be
// mounted (docs/08 §3). It returns HTML, not the API envelope.
func rootHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>HeroPanel</title></head><body style="font-family:system-ui;background:#0b0d10;color:#e6e6e6;display:grid;place-items:center;height:100vh;margin:0"><main style="text-align:center"><h1>HeroPanel</h1><p>Control plane is running. API at <code>/api/v1</code>.</p></main></body></html>`))
	}
}
