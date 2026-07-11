package repository

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// SiteStore implements site.Repo over the datastore.
type SiteStore struct {
	db *DB
}

// NewSiteStore constructs a SiteStore.
func NewSiteStore(db *DB) *SiteStore { return &SiteStore{db: db} }

var _ site.Repo = (*SiteStore)(nil)

const siteSelect = `
	SELECT s.id, s.uid, s.owner_id, s.name, s.primary_domain, s.type, s.deploy_mode,
	       s.status, s.webserver, s.document_root, s.created_at,
	       ssu.linux_user AS linux_user, ssu.linux_uid AS linux_uid, ssu.home_dir AS home_dir
	  FROM sites s
	  LEFT JOIN site_system_users ssu ON ssu.site_id = s.id`

// Insert implements site.Repo.
func (s *SiteStore) Insert(ctx context.Context, r *site.Record) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sites (uid, owner_id, name, primary_domain, type, deploy_mode, status, webserver, document_root)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.OwnerID, r.Name, r.PrimaryDomain, r.Type, r.DeployMode, r.Status, r.Webserver, r.DocumentRoot)
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "site_create_failed", "Could not create the site (domain may be in use).")
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// Provision implements site.Repo.
func (s *SiteStore) Provision(ctx context.Context, p site.ProvisionData) error {
	err := s.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sites SET document_root = ? WHERE id = ?`, p.DocumentRoot, p.SiteID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO site_system_users (site_id, linux_user, linux_uid, home_dir, shell)
			 VALUES (?, ?, ?, ?, ?)`,
			p.SiteID, p.LinuxUser, p.LinuxUID, p.HomeDir, p.Shell); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO domains (uid, site_id, fqdn, kind, force_https) VALUES (?, ?, ?, 'primary', 1)`,
			idgen.NewULID(), p.SiteID, p.PrimaryDomain)
		return err
	})
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// UpdateStatus implements site.Repo.
func (s *SiteStore) UpdateStatus(ctx context.Context, id int64, status string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE sites SET status = ? WHERE id = ?`, status, id); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// GetByUID implements site.Repo.
func (s *SiteStore) GetByUID(ctx context.Context, uid string) (*site.Record, error) {
	var rec site.Record
	err := s.db.GetContext(ctx, &rec, siteSelect+` WHERE s.uid = ? AND s.deleted_at IS NULL`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("site_not_found", "No such site.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

// List implements site.Repo.
func (s *SiteStore) List(ctx context.Context, ownerID int64, limit, offset int) ([]site.Record, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var (
		recs  []site.Record
		query string
		args  []any
	)
	if ownerID > 0 {
		query = siteSelect + ` WHERE s.deleted_at IS NULL AND s.owner_id = ? ORDER BY s.id LIMIT ? OFFSET ?`
		args = []any{ownerID, limit, offset}
	} else {
		query = siteSelect + ` WHERE s.deleted_at IS NULL ORDER BY s.id LIMIT ? OFFSET ?`
		args = []any{limit, offset}
	}
	if err := s.db.SelectContext(ctx, &recs, query, args...); err != nil {
		return nil, errx.Internal(err)
	}
	return recs, nil
}

// SoftDelete implements site.Repo.
func (s *SiteStore) SoftDelete(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sites SET deleted_at = ?, status = 'disabled' WHERE uid = ? AND deleted_at IS NULL`,
		fmtTS(time.Now()), uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}
