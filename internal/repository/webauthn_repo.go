package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// WebAuthnCredential is a stored passkey.
type WebAuthnCredential struct {
	ID           int64  `db:"id"`
	UID          string `db:"uid"`
	UserID       int64  `db:"user_id"`
	CredentialID string `db:"credential_id"` // base64url of raw id
	PublicKey    string `db:"public_key"`    // base64 of COSE key
	SignCount    int64  `db:"sign_count"`
	Name         string `db:"name"`
	CreatedAt    string `db:"created_at"`
}

// WebAuthnRepository persists registered passkeys.
type WebAuthnRepository struct {
	db *DB
}

// NewWebAuthnRepository constructs a WebAuthnRepository.
func NewWebAuthnRepository(db *DB) *WebAuthnRepository { return &WebAuthnRepository{db: db} }

const webauthnSelect = `SELECT id, uid, user_id, credential_id, public_key, sign_count, name, created_at FROM webauthn_credentials`

// Insert records a new credential.
func (r *WebAuthnRepository) Insert(ctx context.Context, c *WebAuthnCredential) error {
	if c.UID == "" {
		c.UID = idgen.NewULID()
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO webauthn_credentials (uid, user_id, credential_id, public_key, sign_count, name, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.UID, c.UserID, c.CredentialID, c.PublicKey, c.SignCount, c.Name, fmtTS(time.Now()))
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "passkey_exists", "That passkey is already registered.")
	}
	if id, err := res.LastInsertId(); err == nil {
		c.ID = id
	}
	return nil
}

// ListByUser returns a user's passkeys, newest first.
func (r *WebAuthnRepository) ListByUser(ctx context.Context, userID int64) ([]WebAuthnCredential, error) {
	var rows []WebAuthnCredential
	if err := r.db.SelectContext(ctx, &rows,
		webauthnSelect+` WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// GetByCredentialID looks a credential (and thus its user) up by the raw
// credential id — the unauthenticated login path.
func (r *WebAuthnRepository) GetByCredentialID(ctx context.Context, credID string) (*WebAuthnCredential, error) {
	var c WebAuthnCredential
	err := r.db.GetContext(ctx, &c, webauthnSelect+` WHERE credential_id = ?`, credID)
	if isNoRows(err) {
		return nil, errx.NotFound("passkey_not_found", "No such passkey.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &c, nil
}

// UpdateSignCount persists the clone-detection counter after a login.
func (r *WebAuthnRepository) UpdateSignCount(ctx context.Context, uid string, count int64) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE webauthn_credentials SET sign_count = ? WHERE uid = ?`, count, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// Delete removes a user's passkey (scoped to the owner).
func (r *WebAuthnRepository) Delete(ctx context.Context, uid string, userID int64) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM webauthn_credentials WHERE uid = ? AND user_id = ?`, uid, userID); err != nil {
		return errx.Internal(err)
	}
	return nil
}
