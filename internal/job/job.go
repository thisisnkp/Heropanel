// Package job is HeroPanel's asynchronous work system. Long or mutating
// operations (site provisioning, backups, deploys) are enqueued as jobs onto a
// Redis Stream, executed by a bounded worker pool with a consumer group
// (at-least-once), and their progress is persisted for polling (and published
// for future WebSocket streaming). See docs/01 §3.5 and ADR-0005.
package job

import (
	"context"
	"database/sql"
)

// Status is a job's lifecycle state.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Job is a unit of asynchronous work.
type Job struct {
	ID          int64          `db:"id"`
	UID         string         `db:"uid"`
	Type        string         `db:"type"`
	Status      string         `db:"status"`
	OwnerUserID sql.NullInt64  `db:"owner_user_id"`
	Progress    int            `db:"progress"`
	Payload     []byte         `db:"payload"`
	Result      []byte         `db:"result"`
	Error       sql.NullString `db:"error"`
	CreatedAt   string         `db:"created_at"`
}

// Progress lets a handler report incremental progress (0-100) and a step label.
type Progress interface {
	Report(pct int, step string)
}

// Handler executes one job. The returned value is JSON-encoded as the job result.
type Handler func(ctx context.Context, j *Job, p Progress) (result any, err error)

// noopProgress discards progress; used by the synchronous execution path.
type noopProgress struct{}

func (noopProgress) Report(int, string) {}

// Noop is a Progress that does nothing.
var Noop Progress = noopProgress{}

// Repo is the job persistence contract (implemented by internal/repository).
type Repo interface {
	Create(ctx context.Context, j *Job) error
	Get(ctx context.Context, uid string) (*Job, error)
	List(ctx context.Context, ownerID int64, limit, offset int) ([]Job, error)
	SetRunning(ctx context.Context, uid string) error
	SetProgress(ctx context.Context, uid string, pct int) error
	Complete(ctx context.Context, uid string, result []byte) error
	Fail(ctx context.Context, uid, errMsg string) error
}
