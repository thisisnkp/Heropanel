package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/monitor"
)

// The Monitoring HTTP edge.
//
// One-shot reads only: the live dashboard is driven by the realtime hub (the
// `monitor:*` channels), not by hitting this endpoint on a timer. This handler
// exists for the initial paint — the first number a page shows before its
// subscription starts delivering — and for anything that wants a single sample
// without opening a socket. Gated by `monitor.read`, the same permission that
// gates the live channels.

// monitorNodeHandler returns one snapshot of node health (CPU, memory, load,
// uptime, disks). Gated by "monitor.read".
func monitorNodeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, d.Monitor.Sample())
	}
}

// monitorSitesHandler returns per-site resource usage read from each site's
// cgroup accounting. Gated by "monitor.read". The live view is the
// `monitor:sites` channel.
func monitorSitesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]any{"sites": d.Monitor.Sites()})
	}
}

// monitorServicesHandler returns the up/down state of the services the host
// depends on. Gated by "monitor.read". The live view is the `monitor:services`
// channel.
func monitorServicesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]any{"services": d.Monitor.Services(r.Context())})
	}
}

// monitorRanges maps the `range` query to a duration. A small allowlist keeps the
// window bounded (an unbounded range would scan the whole table) and picks the
// granularity for the caller — the service reads raw within the raw-retention
// window and hourly beyond it.
var monitorRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// monitorHistoryHandler returns node history over a bounded range. Gated by
// "monitor.read". Query: range (1h|6h|24h|7d|30d, default 24h).
func monitorHistoryHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dur, ok := monitorRanges[r.URL.Query().Get("range")]
		if !ok {
			dur = 24 * time.Hour
		}
		points, err := d.Monitor.History(r.Context(), dur)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"points": points})
	}
}

// ── alert rules (monitor.write) and events (monitor.read) ────────────────────

// listAlertRulesHandler returns the configured rules (notification targets are
// never included — they are write-only). Gated by "monitor.read".
func listAlertRulesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rules, err := d.Monitor.ListRules(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, rules)
	}
}

// createAlertRuleHandler adds a threshold rule. Gated by "monitor.write". A
// webhook or Telegram target is sealed at rest; the "log" kind needs none.
func createAlertRuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in monitor.AlertRuleInput
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "metric", in.Metric)
		audit.AddDetail(r.Context(), "notify_kind", in.NotifyKind)
		// Deliberately not audited: the notify target — it can hold a Telegram token.
		rule, err := d.Monitor.CreateRule(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, rule)
	}
}

// toggleAlertRuleHandler enables or disables a rule. Gated by "monitor.write".
func toggleAlertRuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		audit.AddDetail(r.Context(), "rule", uid)
		if err := d.Monitor.SetRuleEnabled(r.Context(), uid, body.Enabled); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "uid": uid, "enabled": body.Enabled})
	}
}

// deleteAlertRuleHandler removes a rule. Gated by "monitor.write".
func deleteAlertRuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		audit.AddDetail(r.Context(), "rule", uid)
		if err := d.Monitor.DeleteRule(r.Context(), uid); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "uid": uid})
	}
}

// listAlertEventsHandler returns recent firings and resolutions. Gated by
// "monitor.read". Query: limit.
func listAlertEventsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		events, err := d.Monitor.AlertEvents(r.Context(), limit)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, events)
	}
}
