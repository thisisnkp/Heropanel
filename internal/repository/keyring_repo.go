package repository

import (
	"context"

	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/secrets"
)

// KeyringStore persists the rotating data-key envelope (data_keys table): each
// row is a data key wrapped under the master. See pkg/secrets/keyring.go.
type KeyringStore struct {
	db *DB
}

// NewKeyringStore constructs a KeyringStore.
func NewKeyringStore(db *DB) *KeyringStore { return &KeyringStore{db: db} }

// List returns every wrapped data key, oldest generation first.
func (s *KeyringStore) List(ctx context.Context) ([]secrets.WrappedKey, error) {
	var rows []struct {
		Generation int    `db:"generation"`
		Wrapped    string `db:"wrapped"`
	}
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT generation, wrapped FROM data_keys ORDER BY generation`); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]secrets.WrappedKey, len(rows))
	for i, r := range rows {
		out[i] = secrets.WrappedKey{Generation: r.Generation, Wrapped: r.Wrapped}
	}
	return out, nil
}

// Insert records a newly-minted wrapped data key (a data-key rotation).
func (s *KeyringStore) Insert(ctx context.Context, wk secrets.WrappedKey) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO data_keys (generation, wrapped) VALUES (?, ?)`, wk.Generation, wk.Wrapped); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// Replace atomically swaps the whole keyring — used by master rotation, which
// re-wraps every data key under a new master. The generations are unchanged; only
// the wrapped bytes differ.
func (s *KeyringStore) Replace(ctx context.Context, keys []secrets.WrappedKey) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return errx.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM data_keys`); err != nil {
		return errx.Internal(err)
	}
	for _, wk := range keys {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO data_keys (generation, wrapped) VALUES (?, ?)`, wk.Generation, wk.Wrapped); err != nil {
			return errx.Internal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return errx.Internal(err)
	}
	return nil
}
