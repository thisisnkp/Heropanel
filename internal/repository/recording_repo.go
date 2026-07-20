package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/terminal"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// RecordingStore implements terminal.Recordings over the datastore. Only
// metadata lives in the database; the asciicast itself is a file on disk (see
// the 0018 migration).
type RecordingStore struct {
	db *DB
}

// NewRecordingStore constructs a RecordingStore.
func NewRecordingStore(db *DB) *RecordingStore { return &RecordingStore{db: db} }

var _ terminal.Recordings = (*RecordingStore)(nil)

const recordingSelect = `SELECT id, uid, site_id, actor_user_id, actor_email, actor_ip, system_user,
	path, size_bytes, duration_ms, truncated, started_at, ended_at, expires_at
	FROM terminal_recordings`

// Reads that a person looks at carry the site's identity, joined in. Every
// column is qualified because both tables have id/uid/name, and the join is a
// LEFT one guarded by COALESCE: a recording outlives nothing, but a row whose
// site row is gone must still list rather than fail the whole page — losing the
// transcript of a deleted site is precisely the wrong failure mode for an audit
// artifact.
const recordingSelectJoined = `SELECT r.id, r.uid, r.site_id, r.actor_user_id, r.actor_email, r.actor_ip,
	r.system_user, r.path, r.size_bytes, r.duration_ms, r.truncated, r.started_at, r.ended_at, r.expires_at,
	COALESCE(s.uid, '') AS site_uid, COALESCE(s.name, '') AS site_name
	FROM terminal_recordings r LEFT JOIN sites s ON s.id = r.site_id`

// Create opens a recording row at the start of a session, so a session that is
// never cleanly closed still leaves a trace. Finish fills in the rest.
func (s *RecordingStore) Create(ctx context.Context, r *terminal.Recording) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		// started_at is written explicitly rather than left to a column default:
		// the two dialects default it differently, and a recording's start time is
		// not something to leave to the database's idea of "now".
		`INSERT INTO terminal_recordings
		   (uid, site_id, actor_user_id, actor_email, actor_ip, system_user, path, started_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.SiteID, r.ActorUserID, r.ActorEmail, r.ActorIP, r.SystemUser, r.Path, r.StartedAt, r.ExpiresAt)
	if err != nil {
		return errx.Internal(err)
	}
	if id, idErr := res.LastInsertId(); idErr == nil {
		r.ID = id
	}
	return nil
}

// Finish records how the session ended: its size, how long it ran, and whether
// the recording is complete.
func (s *RecordingStore) Finish(ctx context.Context, uid string, size, durationMS int64, truncated bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE terminal_recordings
		    SET size_bytes = ?, duration_ms = ?, truncated = ?, ended_at = ?
		  WHERE uid = ?`,
		size, durationMS, truncated, fmtTS(time.Now()), uid)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// Get returns one recording's metadata.
func (s *RecordingStore) Get(ctx context.Context, uid string) (*terminal.Recording, error) {
	var r terminal.Recording
	err := s.db.GetContext(ctx, &r, recordingSelectJoined+` WHERE r.uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("recording_not_found", "No such terminal recording.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &r, nil
}

// List returns recordings newest first, optionally scoped to one site.
func (s *RecordingStore) List(ctx context.Context, siteID int64, limit, offset int) ([]terminal.Recording, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var (
		out   []terminal.Recording
		query string
		args  []any
	)
	if siteID > 0 {
		query = recordingSelectJoined + ` WHERE r.site_id = ? ORDER BY r.id DESC LIMIT ? OFFSET ?`
		args = []any{siteID, limit, offset}
	} else {
		query = recordingSelectJoined + ` ORDER BY r.id DESC LIMIT ? OFFSET ?`
		args = []any{limit, offset}
	}
	if err := s.db.SelectContext(ctx, &out, query, args...); err != nil {
		return nil, errx.Internal(err)
	}
	return out, nil
}

// Delete removes one recording's row and returns its stored path, so the caller
// can remove the file. The row goes first: a row without a file is a harmless
// dangling reference the UI reports, whereas a file without a row is an
// unreferenced recording nothing will ever clean up.
func (s *RecordingStore) Delete(ctx context.Context, uid string) (string, error) {
	r, err := s.Get(ctx, uid)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM terminal_recordings WHERE uid = ?`, uid); err != nil {
		return "", errx.Internal(err)
	}
	return r.Path, nil
}

// Expired returns recordings past their retention date, for the sweeper.
func (s *RecordingStore) Expired(ctx context.Context, now time.Time, limit int) ([]terminal.Recording, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []terminal.Recording
	err := s.db.SelectContext(ctx, &out,
		recordingSelect+` WHERE expires_at <= ? ORDER BY id ASC LIMIT ?`, fmtTS(now), limit)
	if err != nil {
		return nil, errx.Internal(err)
	}
	return out, nil
}
