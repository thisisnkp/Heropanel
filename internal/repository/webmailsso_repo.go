package repository

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/mail"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// WebmailSSOStore persists the one-time Dovecot master-user sessions that back
// passwordless webmail sign-on. It holds no plaintext secret — only the bcrypt
// hash of a short-lived one-time password, which is enough to re-render the
// master passwd-file.
type WebmailSSOStore struct {
	db *DB
}

// NewWebmailSSOStore constructs the store.
func NewWebmailSSOStore(db *DB) *WebmailSSOStore { return &WebmailSSOStore{db: db} }

var _ mail.WebmailSSORepo = (*WebmailSSOStore)(nil)

// InsertWebmailSSO records a new hand-off session.
func (s *WebmailSSOStore) InsertWebmailSSO(ctx context.Context, r *mail.WebmailSSORecord) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO webmail_sso_sessions (uid, mailbox, master_user, pw_hash, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		r.UID, r.Mailbox, r.MasterUser, r.PWHash, r.ExpiresAt); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ListActiveWebmailSSO returns the sessions that have not yet expired, so the
// master passwd-file can be re-rendered from the live set.
func (s *WebmailSSOStore) ListActiveWebmailSSO(ctx context.Context, now string) ([]mail.WebmailSSORecord, error) {
	var rows []mail.WebmailSSORecord
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT id, uid, mailbox, master_user, pw_hash, created_at, expires_at
		   FROM webmail_sso_sessions WHERE expires_at > ? ORDER BY master_user ASC`, now); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// DeleteExpiredWebmailSSO drops every session past its expiry, returning how
// many were removed.
func (s *WebmailSSOStore) DeleteExpiredWebmailSSO(ctx context.Context, now string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webmail_sso_sessions WHERE expires_at <= ?`, now)
	if err != nil {
		return 0, errx.Internal(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
