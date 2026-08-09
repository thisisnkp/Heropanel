package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/nexpanel/internal/update"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// PanelUpdateStore persists self-update attempts. It is the concrete
// implementation of update.Repo.
type PanelUpdateStore struct {
	db *DB
}

var _ update.Repo = (*PanelUpdateStore)(nil)

// NewPanelUpdateStore constructs a PanelUpdateStore.
func NewPanelUpdateStore(db *DB) *PanelUpdateStore { return &PanelUpdateStore{db: db} }

// COALESCE keeps finished_at a plain string on the Go side: it is NULL for the
// whole time an update is in flight, which is most of the rows this reads.
const panelUpdateCols = `id, uid, from_version, to_version, channel, state, error,
	started_at, COALESCE(finished_at, '')`

func scanPanelUpdate(sc interface{ Scan(...any) error }) (*update.Row, error) {
	var r update.Row
	if err := sc.Scan(&r.ID, &r.UID, &r.FromVersion, &r.ToVersion, &r.Channel,
		&r.State, &r.Error, &r.StartedAt, &r.FinishedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// Insert records a new attempt.
func (s *PanelUpdateStore) Insert(ctx context.Context, r *update.Row) error {
	started := r.StartedAt
	if started == "" {
		started = fmtTS(time.Now())
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO panel_updates (uid, from_version, to_version, channel, state, error, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.FromVersion, r.ToVersion, r.Channel, r.State, r.Error, started)
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	r.StartedAt = started
	return nil
}

// Latest returns the most recent attempt, or (nil, nil) when there has never
// been one — a fresh install has no update history and that is not an error.
func (s *PanelUpdateStore) Latest(ctx context.Context) (*update.Row, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+panelUpdateCols+` FROM panel_updates ORDER BY id DESC LIMIT 1`)
	r, err := scanPanelUpdate(row)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return r, nil
}

// List returns recent attempts, newest first.
func (s *PanelUpdateStore) List(ctx context.Context, limit int) ([]update.Row, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+panelUpdateCols+` FROM panel_updates ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, errx.Internal(err)
	}
	defer func() { _ = rows.Close() }()
	out := []update.Row{}
	for rows.Next() {
		r, err := scanPanelUpdate(rows)
		if err != nil {
			return nil, errx.Internal(err)
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, errx.Internal(err)
	}
	return out, nil
}

// SetState moves an attempt to a new state. finished stamps finished_at, which
// is what distinguishes "this is over" from "this is still running" for the
// panel that comes back up after the swap.
func (s *PanelUpdateStore) SetState(ctx context.Context, uid, state, errMsg string, finished bool) error {
	var fin any
	if finished {
		fin = fmtTS(time.Now())
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE panel_updates SET state = ?, error = ?, finished_at = ? WHERE uid = ?`,
		state, errMsg, fin, uid)
	if err != nil {
		return errx.Internal(err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return errx.NotFound("update_not_found", "No such update attempt.")
	}
	return nil
}
