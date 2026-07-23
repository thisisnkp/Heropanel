package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/cron"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// CronStore implements cron.Repo over the datastore.
type CronStore struct {
	db *DB
}

// NewCronStore constructs a CronStore.
func NewCronStore(db *DB) *CronStore { return &CronStore{db: db} }

var _ cron.Repo = (*CronStore)(nil)

const cronSelect = `SELECT uid, site_id, name, command, schedule, enabled, created_at FROM cron_jobs`

// Insert creates a job row, assigning UID.
func (s *CronStore) Insert(ctx context.Context, r *cron.Record) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_jobs (uid, site_id, name, command, schedule, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
		r.UID, r.SiteID, r.Name, r.Command, r.Schedule, boolToInt(r.Enabled))
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ListBySiteID returns a site's jobs, newest first.
func (s *CronStore) ListBySiteID(ctx context.Context, siteID int64) ([]cron.Record, error) {
	var rows []cronRow
	if err := s.db.SelectContext(ctx, &rows, cronSelect+` WHERE site_id = ? ORDER BY created_at DESC`, siteID); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]cron.Record, len(rows))
	for i := range rows {
		out[i] = rows[i].toRecord()
	}
	return out, nil
}

// GetByUID returns one job.
func (s *CronStore) GetByUID(ctx context.Context, uid string) (*cron.Record, error) {
	var row cronRow
	err := s.db.GetContext(ctx, &row, cronSelect+` WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("job_not_found", "No such scheduled job.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	rec := row.toRecord()
	return &rec, nil
}

// SetEnabled toggles a job.
func (s *CronStore) SetEnabled(ctx context.Context, uid string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cron_jobs SET enabled = ?, updated_at = ? WHERE uid = ?`,
		boolToInt(enabled), fmtTS(time.Now()), uid)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// Delete removes a job.
func (s *CronStore) Delete(ctx context.Context, uid string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE uid = ?`, uid)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// cronRow scans a job; enabled comes back as an int in both dialects.
type cronRow struct {
	UID       string `db:"uid"`
	SiteID    int64  `db:"site_id"`
	Name      string `db:"name"`
	Command   string `db:"command"`
	Schedule  string `db:"schedule"`
	Enabled   int    `db:"enabled"`
	CreatedAt string `db:"created_at"`
}

func (r cronRow) toRecord() cron.Record {
	return cron.Record{
		UID: r.UID, SiteID: r.SiteID, Name: r.Name, Command: r.Command,
		Schedule: r.Schedule, Enabled: r.Enabled == 1, CreatedAt: r.CreatedAt,
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
