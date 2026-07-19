package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/runtime"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// RuntimeStore implements runtime.Repo over the datastore.
type RuntimeStore struct {
	db *DB
}

// NewRuntimeStore constructs a RuntimeStore.
func NewRuntimeStore(db *DB) *RuntimeStore { return &RuntimeStore{db: db} }

var _ runtime.Repo = (*RuntimeStore)(nil)

const runtimeCols = `id, uid, site_id, runtime, command, port, env, health_path, status, created_at, updated_at`

// Upsert writes the site's single runtime, updating in place when present. It
// reloads the row so uid/timestamps are populated. Dialect-agnostic.
func (s *RuntimeStore) Upsert(ctx context.Context, r *runtime.Record) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE app_runtimes SET runtime = ?, command = ?, port = ?, env = ?, health_path = ?, status = ?, updated_at = ?
		 WHERE site_id = ?`,
		r.Runtime, r.Command, r.Port, r.Env, r.HealthPath, r.Status, fmtTS(time.Now()), r.SiteID)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		r.UID = idgen.NewULID()
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO app_runtimes (uid, site_id, runtime, command, port, env, health_path, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.UID, r.SiteID, r.Runtime, r.Command, r.Port, r.Env, r.HealthPath, r.Status); err != nil {
			return errx.Wrap(err, errx.KindConflict, "runtime_exists", "A runtime already exists for this site.")
		}
	}
	got, err := s.GetBySiteID(ctx, r.SiteID)
	if err != nil {
		return err
	}
	*r = *got
	return nil
}

// GetBySiteID returns a site's runtime, or a not-found error.
func (s *RuntimeStore) GetBySiteID(ctx context.Context, siteID int64) (*runtime.Record, error) {
	var rec runtime.Record
	err := s.db.GetContext(ctx, &rec, `SELECT `+runtimeCols+` FROM app_runtimes WHERE site_id = ?`, siteID)
	if isNoRows(err) {
		return nil, errx.NotFound("runtime_not_found", "No runtime is configured for this site.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

// SetStatus updates a runtime's status.
func (s *RuntimeStore) SetStatus(ctx context.Context, siteID int64, status string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE app_runtimes SET status = ?, updated_at = ? WHERE site_id = ?`,
		status, fmtTS(time.Now()), siteID); err != nil {
		return errx.Internal(err)
	}
	return nil
}
