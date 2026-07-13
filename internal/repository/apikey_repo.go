package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// APIKeyRow is a persisted API key. Only the key's hash is stored; the plaintext
// is shown once at creation.
type APIKeyRow struct {
	ID        int64  `db:"id"`
	UID       string `db:"uid"`
	UserID    int64  `db:"user_id"`
	Name      string `db:"name"`
	Prefix    string `db:"prefix"`
	KeyHash   string `db:"key_hash"`
	Scopes    []byte `db:"scopes"`
	CreatedAt string `db:"created_at"`
}

// APIKeyRepository persists API keys.
type APIKeyRepository struct {
	db *DB
}

// NewAPIKeyRepository constructs an APIKeyRepository.
func NewAPIKeyRepository(db *DB) *APIKeyRepository { return &APIKeyRepository{db: db} }

// Create inserts an API key.
func (r *APIKeyRepository) Create(ctx context.Context, k *APIKeyRow) error {
	if len(k.Scopes) == 0 {
		k.Scopes = []byte("[]")
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO api_keys (uid, user_id, name, prefix, key_hash, scopes) VALUES (?, ?, ?, ?, ?, ?)`,
		k.UID, k.UserID, k.Name, k.Prefix, k.KeyHash, k.Scopes)
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		k.ID = id
	}
	return nil
}

// GetActiveByPrefix returns a non-revoked, non-expired key by its prefix.
func (r *APIKeyRepository) GetActiveByPrefix(ctx context.Context, prefix string, now time.Time) (*APIKeyRow, error) {
	var k APIKeyRow
	err := r.db.GetContext(ctx, &k,
		`SELECT id, uid, user_id, name, prefix, key_hash, scopes, created_at
		   FROM api_keys
		  WHERE prefix = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`,
		prefix, fmtTS(now))
	if isNoRows(err) {
		return nil, errx.Unauthorized("invalid_api_key", "Invalid API key.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &k, nil
}

// ListByUser returns a user's non-revoked keys.
func (r *APIKeyRepository) ListByUser(ctx context.Context, userID int64) ([]APIKeyRow, error) {
	var keys []APIKeyRow
	err := r.db.SelectContext(ctx, &keys,
		`SELECT id, uid, user_id, name, prefix, key_hash, scopes, created_at
		   FROM api_keys WHERE user_id = ? AND revoked_at IS NULL ORDER BY id DESC`, userID)
	if err != nil {
		return nil, errx.Internal(err)
	}
	return keys, nil
}

// Revoke marks a user's key revoked.
func (r *APIKeyRepository) Revoke(ctx context.Context, userID int64, uid string, now time.Time) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE uid = ? AND user_id = ? AND revoked_at IS NULL`,
		fmtTS(now), uid, userID); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// TouchLastUsed records the last-used time.
func (r *APIKeyRepository) TouchLastUsed(ctx context.Context, id int64, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, fmtTS(now), id)
	return err
}
