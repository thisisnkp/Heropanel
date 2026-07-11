package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// PHPPoolStore implements php.PoolRepo over the datastore.
type PHPPoolStore struct {
	db *DB
}

// NewPHPPoolStore constructs a PHPPoolStore.
func NewPHPPoolStore(db *DB) *PHPPoolStore { return &PHPPoolStore{db: db} }

var _ php.PoolRepo = (*PHPPoolStore)(nil)

// Upsert inserts or updates a site's pool (keyed by the unique site_id).
func (s *PHPPoolStore) Upsert(ctx context.Context, r *php.PoolRecord) error {
	// Portable upsert: update first; if nothing changed, insert.
	res, err := s.db.ExecContext(ctx,
		`UPDATE php_pools
		    SET php_version = ?, pm = ?, pm_max_children = ?, memory_limit_mb = ?, socket_path = ?, updated_at = ?
		  WHERE site_id = ?`,
		r.PHPVersion, r.PM, r.MaxChildren, r.MemoryLimitMB, r.SocketPath, fmtTS(time.Now()), r.SiteID)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO php_pools (site_id, php_version, pm, pm_max_children, memory_limit_mb, socket_path)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.SiteID, r.PHPVersion, r.PM, r.MaxChildren, r.MemoryLimitMB, r.SocketPath); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// GetBySiteID returns a site's pool.
func (s *PHPPoolStore) GetBySiteID(ctx context.Context, siteID int64) (*php.PoolRecord, error) {
	var rec php.PoolRecord
	err := s.db.GetContext(ctx, &rec,
		`SELECT id, site_id, php_version, pm, pm_max_children, memory_limit_mb, socket_path
		   FROM php_pools WHERE site_id = ?`, siteID)
	if isNoRows(err) {
		return nil, errx.NotFound("php_pool_not_found", "No PHP pool for this site.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}
