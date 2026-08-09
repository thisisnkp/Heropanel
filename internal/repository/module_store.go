package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/nexpanel/internal/marketplace"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// ModuleStore persists the panel's installed-marketplace-module records. It is
// the concrete implementation of marketplace.Store; the marketplace service
// depends on that interface, not on this package.
type ModuleStore struct {
	db *DB
}

// NewModuleStore constructs a ModuleStore.
func NewModuleStore(db *DB) *ModuleStore { return &ModuleStore{db: db} }

// Upsert inserts a module record or, if its slug already exists, refreshes its
// mutable fields — so re-installing a module (a new version, say) updates the
// record in place rather than colliding on the unique slug. state is set only on
// first install; a later Upsert leaves the operator's enable/disable choice
// alone by not overwriting state here.
func (s *ModuleStore) Upsert(ctx context.Context, m marketplace.InstalledModule) error {
	now := fmtTS(time.Now())
	var q string
	if s.db.Dialect == DialectMySQL {
		q = `INSERT INTO modules (slug, name, version, category, state, publisher_key, installed_at, updated_at)
		     VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		     ON DUPLICATE KEY UPDATE name = VALUES(name), version = VALUES(version),
		       category = VALUES(category), publisher_key = VALUES(publisher_key), updated_at = VALUES(updated_at)`
	} else {
		q = `INSERT INTO modules (slug, name, version, category, state, publisher_key, installed_at, updated_at)
		     VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		     ON CONFLICT(slug) DO UPDATE SET name = excluded.name, version = excluded.version,
		       category = excluded.category, publisher_key = excluded.publisher_key, updated_at = excluded.updated_at`
	}
	if _, err := s.db.ExecContext(ctx, q,
		m.Slug, m.Name, m.Version, m.Category, m.State, m.PublisherKey, now, now); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// SetState records an enable/disable transition.
func (s *ModuleStore) SetState(ctx context.Context, slug, state string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE modules SET state = ?, updated_at = ? WHERE slug = ?`, state, fmtTS(time.Now()), slug)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errx.NotFound("module_not_found", "No such installed module.")
	}
	return nil
}

// Get returns one installed-module record, or not-found.
func (s *ModuleStore) Get(ctx context.Context, slug string) (*marketplace.InstalledModule, error) {
	var m marketplace.InstalledModule
	err := s.db.GetContext(ctx, &m,
		`SELECT slug, name, version, category, state, publisher_key, installed_at, updated_at
		   FROM modules WHERE slug = ?`, slug)
	if isNoRows(err) {
		return nil, errx.NotFound("module_not_found", "No such installed module.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &m, nil
}

// List returns every installed-module record, newest install first.
func (s *ModuleStore) List(ctx context.Context) ([]marketplace.InstalledModule, error) {
	var rows []marketplace.InstalledModule
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT slug, name, version, category, state, publisher_key, installed_at, updated_at
		   FROM modules ORDER BY id DESC`); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// Delete removes an installed-module record.
func (s *ModuleStore) Delete(ctx context.Context, slug string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM modules WHERE slug = ?`, slug); err != nil {
		return errx.Internal(err)
	}
	return nil
}
