package httpapi

import (
	"context"
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

// readyHandler is a readiness probe. It checks wired dependencies and returns
// 503 when a required one is unhealthy. Components not yet wired report
// "skipped"; an unconfigured datastore reports "not_configured".
func readyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		components := map[string]string{}
		ready := true

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		checks := []struct {
			name string
			hc   HealthChecker
		}{
			{"database", d.DB},
			{"redis", d.Redis},
			{"broker", d.Broker},
		}
		for _, c := range checks {
			switch {
			case c.hc == nil:
				components[c.name] = "not_configured"
			default:
				if err := c.hc.Health(ctx); err != nil {
					components[c.name] = "error"
					ready = false
				} else {
					components[c.name] = "ok"
				}
			}
		}

		status := http.StatusOK
		state := "ready"
		if !ready {
			status = http.StatusServiceUnavailable
			state = "degraded"
		}
		writeJSON(w, r, status, map[string]any{
			"status":     state,
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

// placeholderHTML is served at "/" when no SPA build is embedded.
const placeholderHTML = `<!doctype html><html><head><meta charset="utf-8"><title>HeroPanel</title></head><body style="font-family:system-ui;background:#0b0d10;color:#e6e6e6;display:grid;place-items:center;height:100vh;margin:0"><main style="text-align:center"><h1>HeroPanel</h1><p>Control plane is running. API at <code>/api/v1</code>.</p><p style="color:#888">Build the UI with <code>npm --prefix web run build</code>.</p></main></body></html>`

func writePlaceholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(placeholderHTML))
}
