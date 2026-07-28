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

// SessionInfo is a session for the management UI. TokenHash is included so the
// service can mark the caller's own session; it is never sent to the client.
type SessionInfo struct {
	UID       string `db:"uid"`
	TokenHash string `db:"token_hash"`
	IP        string `db:"ip"`
	UserAgent string `db:"user_agent"`
	CreatedAt string `db:"created_at"`
	ExpiresAt string `db:"expires_at"`
}

// ListActiveByUser returns a user's active (unrevoked, unexpired) sessions,
// newest first.
func (r *SessionRepository) ListActiveByUser(ctx context.Context, userID int64, now time.Time) ([]SessionInfo, error) {
	var out []SessionInfo
	err := r.db.SelectContext(ctx, &out,
		`SELECT uid, token_hash, ip, user_agent, created_at, expires_at
		   FROM sessions
		  WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
		  ORDER BY created_at DESC`,
		userID, fmtTS(now))
	if err != nil {
		return nil, errx.Internal(err)
	}
	return out, nil
}

// RevokeByUIDForUser revokes one session by UID, scoped to its owner so a
// principal can never revoke another user's session. Returns not-found when the
// UID is not an active session of this user.
func (r *SessionRepository) RevokeByUIDForUser(ctx context.Context, userID int64, uid string, now time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ?
		  WHERE uid = ? AND user_id = ? AND revoked_at IS NULL`,
		fmtTS(now), uid, userID)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errx.NotFound("session_not_found", "No such active session.")
	}
	return nil
}

// RevokeAllForUserExcept revokes every active session of a user except the one
// with exceptHash (the caller's current session) — "sign out everywhere else".
func (r *SessionRepository) RevokeAllForUserExcept(ctx context.Context, userID int64, exceptHash string, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ?
		  WHERE user_id = ? AND token_hash <> ? AND revoked_at IS NULL`,
		fmtTS(now), userID, exceptHash)
	if err != nil {
		return 0, errx.Internal(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
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
