package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// Session is a server-side session row.
type Session struct {
	ID        int64
	UID       string
	UserID    int64
	TokenHash string
	IP        string
	UserAgent string
	ExpiresAt time.Time
}

// SessionRepository persists sessions.
type SessionRepository struct {
	db *DB
}

// NewSessionRepository constructs a SessionRepository.
func NewSessionRepository(db *DB) *SessionRepository { return &SessionRepository{db: db} }

// Create inserts a session, assigning a UID.
func (r *SessionRepository) Create(ctx context.Context, s *Session) error {
	if s.UID == "" {
		s.UID = idgen.NewULID()
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (uid, user_id, token_hash, ip, user_agent, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		s.UID, s.UserID, s.TokenHash, s.IP, s.UserAgent, fmtTS(s.ExpiresAt))
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		s.ID = id
	}
	return nil
}

// UserIDForActiveToken returns the user id for a session that is not revoked and
// not expired as of now. It returns an unauthorized error when no such session
// exists.
func (r *SessionRepository) UserIDForActiveToken(ctx context.Context, tokenHash string, now time.Time) (int64, error) {
	var userID int64
	err := r.db.GetContext(ctx, &userID,
		`SELECT user_id FROM sessions
		  WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ?`,
		tokenHash, fmtTS(now))
	if isNoRows(err) {
		return 0, errx.Unauthorized("invalid_session", "Session is invalid or expired.")
	}
	if err != nil {
		return 0, errx.Internal(err)
	}
	return userID, nil
}

// Revoke marks the session with the given token hash as revoked.
func (r *SessionRepository) Revoke(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`,
		fmtTS(now), tokenHash)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteExpired removes sessions that expired before now (housekeeping).
func (r *SessionRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, fmtTS(now))
	if err != nil {
		return 0, errx.Internal(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
