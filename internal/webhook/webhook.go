// Package webhook delivers outbound event notifications. It taps the audit
// stream — the panel's canonical "what happened" log — so every mutation is a
// candidate event without instrumenting each service. A subscription names an
// endpoint, the resource types it wants, and a signing secret; a background
// dispatcher POSTs matching events, HMAC-signed, with retry/backoff, and records
// each attempt.
//
// Tenancy holds here too: a non-superuser's webhook only receives events whose
// actor is inside that owner's tenant subtree, so a reseller cannot subscribe to
// the whole platform's activity.
package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/tenancy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Service manages webhook subscriptions and turns audit entries into deliveries.
type Service struct {
	store   *repository.WebhookStore
	tenancy *tenancy.Resolver
	rbac    *repository.RBACRepository
	log     *slog.Logger
	now     func() time.Time
}

// NewService constructs the webhook service.
func NewService(store *repository.WebhookStore, ten *tenancy.Resolver, rbac *repository.RBACRepository, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, tenancy: ten, rbac: rbac, log: log, now: time.Now}
}

// Available reports whether the module can operate.
func (s *Service) Available() bool { return s != nil && s.store != nil }

// View is the API representation of a subscription (never carries the secret).
type View struct {
	UID       string   `json:"uid"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Active    bool     `json:"active"`
	CreatedAt string   `json:"created_at"`
}

func viewOf(r repository.WebhookRow) View {
	return View{UID: r.UID, URL: r.URL, Events: r.Events, Active: r.Active, CreatedAt: r.CreatedAt}
}

// Created is returned once on creation: the view plus the plaintext signing
// secret, which is never retrievable again.
type Created struct {
	View
	Secret string `json:"secret"`
}

// Create validates and stores a subscription, minting a signing secret returned
// once. events defaults to ["*"] (all resource types).
func (s *Service) Create(ctx context.Context, ownerID int64, rawURL string, events []string) (*Created, error) {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errx.Validation("invalid_url", "Enter a valid http(s) URL.")
	}
	events = normalizeEvents(events)
	secret, err := newSecret()
	if err != nil {
		return nil, errx.Internal(err)
	}
	uid, err := s.store.CreateWebhook(ctx, ownerID, rawURL, secret, events)
	if err != nil {
		return nil, err
	}
	return &Created{
		View:   View{UID: uid, URL: rawURL, Events: events, Active: true, CreatedAt: s.now().UTC().Format("2006-01-02 15:04:05")},
		Secret: secret,
	}, nil
}

// List returns the subscriptions visible to the caller: all for a superuser, or
// the caller's tenant subtree otherwise.
func (s *Service) List(ctx context.Context, superuser bool, ownerIDs []int64) ([]View, error) {
	var (
		rows []repository.WebhookRow
		err  error
	)
	if superuser {
		rows, err = s.store.ListAllWebhooks(ctx)
	} else {
		rows, err = s.store.ListWebhooksForOwners(ctx, ownerIDs)
	}
	if err != nil {
		return nil, err
	}
	out := make([]View, len(rows))
	for i := range rows {
		out[i] = viewOf(rows[i])
	}
	return out, nil
}

// Delete removes a subscription the caller may see.
func (s *Service) Delete(ctx context.Context, uid string, ownerScope []int64) error {
	row, err := s.store.GetWebhookForOwner(ctx, uid, ownerScope)
	if err != nil {
		return err
	}
	return s.store.DeleteWebhook(ctx, row.ID)
}

// Deliveries returns recent delivery attempts for a subscription the caller may see.
func (s *Service) Deliveries(ctx context.Context, uid string, ownerScope []int64, limit int) ([]repository.WebhookDelivery, error) {
	row, err := s.store.GetWebhookForOwner(ctx, uid, ownerScope)
	if err != nil {
		return nil, err
	}
	return s.store.ListDeliveries(ctx, row.ID, limit)
}

// OnAuditEntry is the audit observer: it fans a committed entry out to every
// active, matching, entitled subscription as a pending delivery. It runs on the
// audit service's goroutine, detached from the request, so it takes its own
// context and never propagates an error back to the writer.
func (s *Service) OnAuditEntry(e audit.Entry) {
	if s == nil || s.store == nil {
		return
	}
	// Denied attempts (failed authn/authz) are deliberately not broadcast — they
	// are security noise and can leak probing to a subscriber.
	if e.Outcome == audit.OutcomeDenied {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hooks, err := s.store.ActiveWebhooks(ctx)
	if err != nil {
		s.log.Error("webhook: could not load subscriptions", "err", err)
		return
	}
	if len(hooks) == 0 {
		return
	}
	payload := s.buildPayload(e)
	for _, h := range hooks {
		if !eventMatches(h.Events, e.ResourceType) {
			continue
		}
		ok, err := s.entitled(ctx, h.OwnerID, e.ActorUserID)
		if err != nil {
			s.log.Error("webhook: entitlement check failed", "webhook", h.UID, "err", err)
			continue
		}
		if !ok {
			continue
		}
		if err := s.store.EnqueueDelivery(ctx, h.ID, eventName(e), e.ResourceType, e.ResourceID, payload, s.now()); err != nil {
			s.log.Error("webhook: enqueue failed", "webhook", h.UID, "err", err)
		}
	}
}

// entitled reports whether a subscription owner may receive an event acted by
// actorID: a superuser receives everything; otherwise the actor must be inside
// the owner's tenant subtree.
func (s *Service) entitled(ctx context.Context, ownerID, actorID int64) (bool, error) {
	if s.rbac != nil {
		super, err := s.rbac.UserHoldsWildcard(ctx, ownerID)
		if err != nil {
			return false, err
		}
		if super {
			return true, nil
		}
	}
	if actorID == 0 {
		return false, nil // a system/anonymous actor belongs to no tenant
	}
	if s.tenancy == nil {
		return false, nil
	}
	return s.tenancy.CanAccessOwner(ctx, ownerID, actorID)
}

// buildPayload renders the signed JSON body for an entry.
func (s *Service) buildPayload(e audit.Entry) string {
	var detail any
	if e.Detail != "" {
		_ = json.Unmarshal([]byte(e.Detail), &detail)
	}
	body := map[string]any{
		"event":         eventName(e),
		"action":        e.Action,
		"resource_type": e.ResourceType,
		"resource_id":   e.ResourceID,
		"outcome":       string(e.Outcome),
		"actor_kind":    string(e.ActorKind),
		"occurred_at":   e.CreatedAt,
		"audit_uid":     e.UID,
		"detail":        detail,
	}
	b, _ := json.Marshal(body)
	return string(b)
}

// eventName is the event label: "<resource_type>.<outcome>", or the action when
// no resource type was recorded.
func eventName(e audit.Entry) string {
	if e.ResourceType == "" {
		return e.Action
	}
	return e.ResourceType + "." + string(e.Outcome)
}

// eventMatches reports whether a subscription's event filter covers a resource
// type. "*" matches everything.
func eventMatches(events []string, resourceType string) bool {
	for _, ev := range events {
		if ev == "*" || ev == resourceType {
			return true
		}
	}
	return false
}

// normalizeEvents trims, de-duplicates, and defaults to ["*"].
func normalizeEvents(events []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, e := range events {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}

func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + hex.EncodeToString(b), nil
}
