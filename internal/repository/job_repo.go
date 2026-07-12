package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// JobStore implements job.Repo over the datastore.
type JobStore struct {
	db *DB
}

// NewJobStore constructs a JobStore.
func NewJobStore(db *DB) *JobStore { return &JobStore{db: db} }

var _ job.Repo = (*JobStore)(nil)

const jobSelect = `SELECT id, uid, type, status, owner_user_id, progress, payload, result, error, created_at FROM jobs`

// Create inserts a queued job, assigning a UID.
func (s *JobStore) Create(ctx context.Context, j *job.Job) error {
	if j.UID == "" {
		j.UID = idgen.NewULID()
	}
	if len(j.Payload) == 0 {
		j.Payload = []byte("{}")
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (uid, type, status, owner_user_id, progress, payload, result)
		 VALUES (?, ?, ?, ?, 0, ?, '{}')`,
		j.UID, j.Type, j.Status, j.OwnerUserID, j.Payload)
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		j.ID = id
	}
	return nil
}

// Get returns a job by UID.
func (s *JobStore) Get(ctx context.Context, uid string) (*job.Job, error) {
	var j job.Job
	err := s.db.GetContext(ctx, &j, jobSelect+` WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("job_not_found", "No such job.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &j, nil
}

// List returns jobs (all owners when ownerID is 0), newest first.
func (s *JobStore) List(ctx context.Context, ownerID int64, limit, offset int) ([]job.Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var (
		jobs  []job.Job
		query string
		args  []any
	)
	if ownerID > 0 {
		query = jobSelect + ` WHERE owner_user_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`
		args = []any{ownerID, limit, offset}
	} else {
		query = jobSelect + ` ORDER BY id DESC LIMIT ? OFFSET ?`
		args = []any{limit, offset}
	}
	if err := s.db.SelectContext(ctx, &jobs, query, args...); err != nil {
		return nil, errx.Internal(err)
	}
	return jobs, nil
}

// SetRunning marks a job running and records its start time.
func (s *JobStore) SetRunning(ctx context.Context, uid string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'running', started_at = ? WHERE uid = ?`, fmtTS(time.Now()), uid)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// SetProgress updates a job's progress percentage.
func (s *JobStore) SetProgress(ctx context.Context, uid string, pct int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET progress = ? WHERE uid = ?`, pct, uid)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// Complete marks a job succeeded with its result.
func (s *JobStore) Complete(ctx context.Context, uid string, result []byte) error {
	if len(result) == 0 {
		result = []byte("{}")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'succeeded', progress = 100, result = ?, finished_at = ? WHERE uid = ?`,
		result, fmtTS(time.Now()), uid)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// Fail marks a job failed with an error message.
func (s *JobStore) Fail(ctx context.Context, uid, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'failed', error = ?, finished_at = ? WHERE uid = ?`,
		errMsg, fmtTS(time.Now()), uid)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}
