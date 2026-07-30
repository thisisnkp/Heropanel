package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
	"github.com/thisisnkp/heropanel/pkg/secrets"
)

// WebhookStore persists outbound webhook subscriptions and their delivery queue.
// The signing secret is sealed at rest with the panel data key, mirroring how
// alert targets and Git credentials are stored.
type WebhookStore struct {
	db     *DB
	cipher *secrets.Cipher
}

// NewWebhookStore constructs a WebhookStore. cipher may be nil/unconfigured, in
// which case creating a webhook is refused (there is nowhere safe to keep its
// secret).
func NewWebhookStore(db *DB, cipher *secrets.Cipher) *WebhookStore {
	return &WebhookStore{db: db, cipher: cipher}
}

// WebhookRow is a subscription without its secret — the shape lists and matching
// need.
type WebhookRow struct {
	ID        int64
	UID       string
	OwnerID   int64
	URL       string
	Events    []string
	Active    bool
	CreatedAt string
}

// WebhookDeliveryJob is a due delivery joined with the endpoint and its opened
// signing secret, ready for the dispatcher to sign and POST.
type WebhookDeliveryJob struct {
	DeliveryID int64
	WebhookUID string
	URL        string
	Secret     string
	Event      string
	Payload    string
	Attempts   int
}

// WebhookDelivery is the API view of a delivery attempt (no payload body).
type WebhookDelivery struct {
	UID          string `db:"uid" json:"uid"`
	Event        string `db:"event" json:"event"`
	ResourceType string `db:"resource_type" json:"resource_type"`
	ResourceID   string `db:"resource_id" json:"resource_id"`
	Status       string `db:"status" json:"status"`
	Attempts     int    `db:"attempts" json:"attempts"`
	ResponseCode int    `db:"response_code" json:"response_code"`
	Error        string `db:"error" json:"error"`
	CreatedAt    string `db:"created_at" json:"created_at"`
	DeliveredAt  string `db:"delivered_at" json:"delivered_at"`
}

func webhookAAD(uid string) string { return "webhook:" + uid }

// CreateWebhook seals the secret and inserts a subscription, returning its uid.
func (s *WebhookStore) CreateWebhook(ctx context.Context, ownerID int64, url, secret string, events []string) (string, error) {
	if s.cipher == nil || !s.cipher.Configured() {
		return "", errx.New(errx.KindUnavailable, "secrets_unavailable",
			"A data key (HP_SECRET_KEY) is required to store a webhook secret.")
	}
	uid := idgen.NewULID()
	sealed, err := s.cipher.Seal([]byte(secret), webhookAAD(uid))
	if err != nil {
		return "", errx.Internal(err)
	}
	evJSON, _ := json.Marshal(events)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO webhooks (uid, owner_id, url, secret_enc, events, active) VALUES (?, ?, ?, ?, ?, 1)`,
		uid, ownerID, url, sealed, string(evJSON)); err != nil {
		return "", errx.Internal(err)
	}
	return uid, nil
}

type webhookScanRow struct {
	ID        int64  `db:"id"`
	UID       string `db:"uid"`
	OwnerID   int64  `db:"owner_id"`
	URL       string `db:"url"`
	Events    string `db:"events"`
	Active    int    `db:"active"`
	CreatedAt string `db:"created_at"`
}

func (r webhookScanRow) toRow() WebhookRow {
	var events []string
	_ = json.Unmarshal([]byte(r.Events), &events)
	if events == nil {
		events = []string{}
	}
	return WebhookRow{ID: r.ID, UID: r.UID, OwnerID: r.OwnerID, URL: r.URL, Events: events, Active: r.Active == 1, CreatedAt: r.CreatedAt}
}

// ListWebhooksForOwners returns the subscriptions owned by any of ownerIDs.
func (s *WebhookStore) ListWebhooksForOwners(ctx context.Context, ownerIDs []int64) ([]WebhookRow, error) {
	if len(ownerIDs) == 0 {
		return []WebhookRow{}, nil
	}
	q, args, err := sqlx.In(
		`SELECT id, uid, owner_id, url, events, active, created_at FROM webhooks WHERE owner_id IN (?) ORDER BY id DESC`, ownerIDs)
	if err != nil {
		return nil, errx.Internal(err)
	}
	var rows []webhookScanRow
	if err := s.db.SelectContext(ctx, &rows, s.db.Rebind(q), args...); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]WebhookRow, len(rows))
	for i := range rows {
		out[i] = rows[i].toRow()
	}
	return out, nil
}

// ListAllWebhooks returns every subscription (for a superuser's listing).
func (s *WebhookStore) ListAllWebhooks(ctx context.Context) ([]WebhookRow, error) {
	var rows []webhookScanRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT id, uid, owner_id, url, events, active, created_at FROM webhooks ORDER BY id DESC`); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]WebhookRow, len(rows))
	for i := range rows {
		out[i] = rows[i].toRow()
	}
	return out, nil
}

// ActiveWebhooks returns every active subscription (id, owner, events) for the
// dispatcher's fan-out matching. Secrets are not opened here — signing happens
// at delivery.
func (s *WebhookStore) ActiveWebhooks(ctx context.Context) ([]WebhookRow, error) {
	var rows []webhookScanRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT id, uid, owner_id, url, events, active, created_at FROM webhooks WHERE active = 1`); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]WebhookRow, len(rows))
	for i := range rows {
		out[i] = rows[i].toRow()
	}
	return out, nil
}

// GetWebhookForOwner returns one subscription scoped to an owner set (nil = any,
// for a superuser). Returns not-found when the uid is not visible.
func (s *WebhookStore) GetWebhookForOwner(ctx context.Context, uid string, ownerIDs []int64) (*WebhookRow, error) {
	var row webhookScanRow
	var err error
	if ownerIDs == nil {
		err = s.db.GetContext(ctx, &row,
			`SELECT id, uid, owner_id, url, events, active, created_at FROM webhooks WHERE uid = ?`, uid)
	} else {
		if len(ownerIDs) == 0 {
			return nil, errx.NotFound("webhook_not_found", "No such webhook.")
		}
		q, args, ierr := sqlx.In(
			`SELECT id, uid, owner_id, url, events, active, created_at FROM webhooks WHERE uid = ? AND owner_id IN (?)`, uid, ownerIDs)
		if ierr != nil {
			return nil, errx.Internal(ierr)
		}
		err = s.db.GetContext(ctx, &row, s.db.Rebind(q), args...)
	}
	if isNoRows(err) {
		return nil, errx.NotFound("webhook_not_found", "No such webhook.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	r := row.toRow()
	return &r, nil
}

// DeleteWebhook removes a subscription (and its deliveries via cascade).
func (s *WebhookStore) DeleteWebhook(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// EnqueueDelivery records a pending delivery for a subscription.
func (s *WebhookStore) EnqueueDelivery(ctx context.Context, webhookID int64, event, resourceType, resourceID, payload string, now time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (uid, webhook_id, event, resource_type, resource_id, payload, status, next_attempt_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)`,
		idgen.NewULID(), webhookID, event, resourceType, resourceID, payload, fmtTS(now)); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DueDeliveries returns up to limit pending deliveries whose next attempt is due,
// each joined with its endpoint and opened signing secret.
func (s *WebhookStore) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]WebhookDeliveryJob, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []struct {
		DeliveryID int64  `db:"delivery_id"`
		WebhookUID string `db:"webhook_uid"`
		URL        string `db:"url"`
		SecretEnc  string `db:"secret_enc"`
		Event      string `db:"event"`
		Payload    string `db:"payload"`
		Attempts   int    `db:"attempts"`
	}
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT d.id AS delivery_id, w.uid AS webhook_uid, w.url AS url, w.secret_enc AS secret_enc,
		        d.event AS event, d.payload AS payload, d.attempts AS attempts
		   FROM webhook_deliveries d
		   JOIN webhooks w ON w.id = d.webhook_id
		  WHERE d.status = 'pending' AND d.next_attempt_at <= ? AND w.active = 1
		  ORDER BY d.id ASC LIMIT ?`, fmtTS(now), limit); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]WebhookDeliveryJob, 0, len(rows))
	for _, r := range rows {
		secret := ""
		if s.cipher != nil {
			if raw, err := s.cipher.Open(r.SecretEnc, webhookAAD(r.WebhookUID)); err == nil {
				secret = string(raw)
			}
		}
		out = append(out, WebhookDeliveryJob{
			DeliveryID: r.DeliveryID, WebhookUID: r.WebhookUID, URL: r.URL, Secret: secret,
			Event: r.Event, Payload: r.Payload, Attempts: r.Attempts,
		})
	}
	return out, nil
}

// MarkDelivered finalizes a delivery as succeeded.
func (s *WebhookStore) MarkDelivered(ctx context.Context, id int64, code int, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = 'delivered', attempts = attempts + 1, response_code = ?, error = '', delivered_at = ? WHERE id = ?`,
		code, fmtTS(now), id)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// MarkFailed records a failed attempt: it re-queues with next_attempt_at when
// more retries remain, or marks the delivery failed when exhausted.
func (s *WebhookStore) MarkFailed(ctx context.Context, id, code int64, errMsg string, nextAttempt time.Time, exhausted bool) error {
	status := "pending"
	if exhausted {
		status = "failed"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, attempts = attempts + 1, response_code = ?, error = ?, next_attempt_at = ? WHERE id = ?`,
		status, code, errMsg, fmtTS(nextAttempt), id)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ListDeliveries returns recent delivery attempts for a subscription.
func (s *WebhookStore) ListDeliveries(ctx context.Context, webhookID int64, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []WebhookDelivery
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT uid, event, resource_type, resource_id, status, attempts, response_code, error, created_at,
		        COALESCE(delivered_at, '') AS delivered_at
		   FROM webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT ?`, webhookID, limit); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}
