package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/webhook"
)

// Outbound webhooks edge. A webhook is an owned resource (webhook.read /
// webhook.write) tenant-scoped like the rest: a superuser sees all, everyone
// else sees their own subtree. The signing secret is returned once on create.

// webhookScope returns the owner scope for a webhook read/mutate: nil for a
// superuser (any owner), otherwise the caller's visible owner set.
func webhookScope(d Deps, r *http.Request) ([]int64, error) {
	super, owners, err := visibleOwners(d, r)
	if err != nil {
		return nil, err
	}
	if super {
		return nil, nil
	}
	return owners, nil
}

// listWebhooksHandler lists the caller's webhooks. Gated by "webhook.read".
func listWebhooksHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		super, owners, err := visibleOwners(d, r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		list, err := d.Webhooks.List(r.Context(), super, owners)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if list == nil {
			list = []webhook.View{}
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"webhooks": list})
	}
}

// createWebhookHandler registers a webhook for the caller. Gated by "webhook.write".
func createWebhookHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			URL    string   `json:"url"`
			Events []string `json:"events"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		out, err := d.Webhooks.Create(r.Context(), p.UserID, req.URL, req.Events)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "webhooks", out.UID)
		audit.AddDetail(r.Context(), "url", out.URL)
		audit.AddDetail(r.Context(), "events", out.Events)
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// deleteWebhookHandler removes one of the caller's webhooks. Gated by "webhook.write".
func deleteWebhookHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, err := webhookScope(d, r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if err := d.Webhooks.Delete(r.Context(), chi.URLParam(r, "uid"), scope); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// listWebhookDeliveriesHandler returns recent delivery attempts. Gated by "webhook.read".
func listWebhookDeliveriesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, err := webhookScope(d, r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out, err := d.Webhooks.Deliveries(r.Context(), chi.URLParam(r, "uid"), scope, 50)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out == nil {
			out = []repository.WebhookDelivery{}
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"deliveries": out})
	}
}
