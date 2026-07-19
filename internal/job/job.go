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

// scaledProgress maps an inner 0-100 range onto an outer [lo, hi] window.
type scaledProgress struct {
	inner  Progress
	lo, hi int
}

// ScaleProgress adapts a Progress so that one job can delegate to a routine that
// reports 0-100 for itself, without that routine's "100%" meaning the whole job
// is finished.
//
// Cloning a site is the motivating case: it calls the create flow, which
// legitimately reports its own progress from 0 to 100, and the bar would run to
// the end and then sit there while the actual copy — the part the operator is
// waiting on — had not started. Scaling create into [10, 70] keeps the bar
// honest about how much is left.
func ScaleProgress(p Progress, lo, hi int) Progress {
	if p == nil {
		return Noop
	}
	if lo < 0 {
		lo = 0
	}
	if hi > 100 {
		hi = 100
	}
	if hi <= lo {
		// A window that cannot represent progress: report nothing rather than
		// send the bar backwards.
		return Noop
	}
	return scaledProgress{inner: p, lo: lo, hi: hi}
}

func (s scaledProgress) Report(pct int, step string) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	s.inner.Report(s.lo+(pct*(s.hi-s.lo))/100, step)
}

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
