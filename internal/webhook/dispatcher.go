package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/thisisnkp/nexpanel/internal/repository"
)

// Delivery tuning. Attempts follow an exponential backoff, capped, so a briefly
// unreachable endpoint recovers without a thundering retry, and a permanently
// broken one stops after maxAttempts instead of retrying forever.
const (
	maxAttempts     = 6
	baseBackoff     = 30 * time.Second
	maxBackoff      = 30 * time.Minute
	deliveryTimeout = 10 * time.Second
	batchSize       = 50

	// Signature headers. The signed string is "<timestamp>.<body>", so a captured
	// body cannot be replayed under a new timestamp without invalidating the HMAC.
	headerEvent     = "X-NexPanel-Event"
	headerDelivery  = "X-NexPanel-Delivery"
	headerTimestamp = "X-NexPanel-Timestamp"
	headerSignature = "X-NexPanel-Signature"
)

// Dispatcher drains the delivery queue: it signs and POSTs due deliveries and
// records the outcome, retrying with backoff.
type Dispatcher struct {
	store  *repository.WebhookStore
	client *http.Client
	log    *slog.Logger
	now    func() time.Time
	tick   time.Duration
}

// NewDispatcher constructs a Dispatcher.
func NewDispatcher(store *repository.WebhookStore, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{
		store:  store,
		client: &http.Client{Timeout: deliveryTimeout},
		log:    log,
		now:    time.Now,
		tick:   15 * time.Second,
	}
}

// Run drains the queue on a ticker until ctx is cancelled. Intended to be
// launched in its own goroutine at bootstrap.
func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.drain(ctx)
		}
	}
}

// drain processes one batch of due deliveries.
func (d *Dispatcher) drain(ctx context.Context) {
	jobs, err := d.store.DueDeliveries(ctx, d.now(), batchSize)
	if err != nil {
		d.log.Error("webhook: could not load due deliveries", "err", err)
		return
	}
	for _, j := range jobs {
		d.deliver(ctx, j)
	}
}

// deliver signs and POSTs one delivery, then records the result.
func (d *Dispatcher) deliver(ctx context.Context, j repository.WebhookDeliveryJob) {
	code, err := d.post(ctx, j)
	now := d.now()
	if err == nil && code >= 200 && code < 300 {
		if merr := d.store.MarkDelivered(ctx, j.DeliveryID, code, now); merr != nil {
			d.log.Error("webhook: mark delivered failed", "delivery", j.DeliveryID, "err", merr)
		}
		return
	}
	attempt := j.Attempts + 1
	exhausted := attempt >= maxAttempts
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	next := now.Add(backoffFor(attempt))
	if merr := d.store.MarkFailed(ctx, int64(j.DeliveryID), int64(code), msg, next, exhausted); merr != nil {
		d.log.Error("webhook: mark failed failed", "delivery", j.DeliveryID, "err", merr)
	}
}

// post signs and sends the request, returning the HTTP status (0 on transport
// error).
func (d *Dispatcher) post(ctx context.Context, j repository.WebhookDeliveryJob) (int, error) {
	ts := strconv.FormatInt(d.now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.URL, bytes.NewReader([]byte(j.Payload)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NexPanel-Webhook/1")
	req.Header.Set(headerEvent, j.Event)
	req.Header.Set(headerDelivery, j.WebhookUID)
	req.Header.Set(headerTimestamp, ts)
	req.Header.Set(headerSignature, Sign(j.Secret, ts, j.Payload))

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// Sign computes the delivery signature: "sha256=" + HMAC-SHA256(secret,
// "<timestamp>.<payload>"). Consumers recompute it to verify authenticity and
// integrity.
func Sign(secret, timestamp, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// backoffFor returns the delay before the given attempt number (1-based),
// doubling from baseBackoff and capped at maxBackoff.
func backoffFor(attempt int) time.Duration {
	d := baseBackoff
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= maxBackoff {
			return maxBackoff
		}
	}
	return d
}
