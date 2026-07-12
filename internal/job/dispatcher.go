package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/redis/go-redis/v9"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

const (
	defaultStream = "hp:jobs"
	consumerGroup = "workers"
)

// Dispatcher enqueues jobs onto the Redis Stream, holds the handler registry,
// and (via worker.go) runs the consuming worker pool.
type Dispatcher struct {
	repo   Repo
	rdb    *redis.Client
	stream string
	group  string
	log    *slog.Logger

	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewDispatcher constructs a Dispatcher.
func NewDispatcher(repo Repo, rdb *redis.Client, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{
		repo:     repo,
		rdb:      rdb,
		stream:   defaultStream,
		group:    consumerGroup,
		log:      log,
		handlers: map[string]Handler{},
	}
}

// Register associates a handler with a job type. It panics on duplicate
// registration (a startup programmer error).
func (d *Dispatcher) Register(jobType string, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handlers[jobType]; exists {
		panic("job: duplicate handler for type " + jobType)
	}
	d.handlers[jobType] = h
}

func (d *Dispatcher) handler(jobType string) (Handler, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	h, ok := d.handlers[jobType]
	return h, ok
}

// Enqueue persists a queued job and publishes it to the stream for a worker to
// pick up. ownerID 0 means "no owner".
func (d *Dispatcher) Enqueue(ctx context.Context, jobType string, ownerID int64, payload any) (*Job, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, errx.Wrap(err, errx.KindValidation, "bad_payload", "Could not encode the job payload.")
	}
	j := &Job{
		UID:     idgen.NewULID(),
		Type:    jobType,
		Status:  string(StatusQueued),
		Payload: raw,
	}
	if ownerID > 0 {
		j.OwnerUserID = sql.NullInt64{Int64: ownerID, Valid: true}
	}
	if err := d.repo.Create(ctx, j); err != nil {
		return nil, err
	}
	if err := d.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: d.stream,
		Values: map[string]any{"job": j.UID},
	}).Err(); err != nil {
		// The job row exists but was not queued; mark it failed so it is not
		// left dangling in "queued" forever.
		_ = d.repo.Fail(ctx, j.UID, "could not enqueue to the job stream")
		return nil, errx.Wrap(err, errx.KindUnavailable, "enqueue_failed", "Could not enqueue the job.")
	}
	return j, nil
}

// Get returns a job by UID.
func (d *Dispatcher) Get(ctx context.Context, uid string) (*Job, error) {
	return d.repo.Get(ctx, uid)
}

// List returns jobs for an owner (0 = all), newest first.
func (d *Dispatcher) List(ctx context.Context, ownerID int64, limit, offset int) ([]Job, error) {
	return d.repo.List(ctx, ownerID, limit, offset)
}
