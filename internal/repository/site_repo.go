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
	       s.status, s.webserver, s.document_root, s.app_project, s.created_at,
	       ssu.linux_user AS linux_user, ssu.linux_uid AS linux_uid, ssu.home_dir AS home_dir
	  FROM sites s
	  LEFT JOIN site_system_users ssu ON ssu.site_id = s.id`

// Insert implements site.Repo.
func (s *SiteStore) Insert(ctx context.Context, r *site.Record) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sites (uid, owner_id, name, primary_domain, type, deploy_mode, status, webserver, document_root, app_project)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.OwnerID, r.Name, r.PrimaryDomain, r.Type, r.DeployMode, r.Status, r.Webserver, r.DocumentRoot, r.AppProject)
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
		// force_https starts off: forcing HTTPS before a certificate exists would
		// take the new site offline. It is enabled explicitly via the domains API.
		_, err := tx.ExecContext(ctx,
			`INSERT INTO domains (uid, site_id, fqdn, kind, force_https) VALUES (?, ?, ?, 'primary', 0)`,
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

// GetByID returns a site by internal id (used by background sweeps that hold an
// id from a config row rather than a uid).
func (s *SiteStore) GetByID(ctx context.Context, id int64) (*site.Record, error) {
	var rec site.Record
	err := s.db.GetContext(ctx, &rec, siteSelect+` WHERE s.id = ? AND s.deleted_at IS NULL`, id)
	if isNoRows(err) {
		return nil, errx.NotFound("site_not_found", "No such site.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

// GetByAppProject implements site.Repo.
func (s *SiteStore) GetByAppProject(ctx context.Context, project string) (*site.Record, error) {
	var rec site.Record
	err := s.db.GetContext(ctx, &rec,
		siteSelect+` WHERE s.app_project = ? AND s.deleted_at IS NULL`, project)
	if isNoRows(err) {
		return nil, errx.NotFound("site_not_found", "That app is not exposed to a domain.")
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

// GetLimits returns a site's resource limits. A site that has never been given
// any has no row, which is not an error — it means "unlimited", so the zero
// value is returned.
func (s *SiteStore) GetLimits(ctx context.Context, siteID int64) (*site.Limits, error) {
	var l site.Limits
	err := s.db.GetContext(ctx, &l,
		`SELECT cpu_quota_pct, mem_limit_bytes, pids_max FROM site_limits WHERE site_id = ?`, siteID)
	if isNoRows(err) {
		return &site.Limits{}, nil
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &l, nil
}

// UpsertLimits writes a site's resource limits (portable update-then-insert).
func (s *SiteStore) UpsertLimits(ctx context.Context, siteID int64, l site.Limits) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE site_limits SET cpu_quota_pct = ?, mem_limit_bytes = ?, pids_max = ?, updated_at = ?
		 WHERE site_id = ?`,
		l.CPUQuotaPct, l.MemLimitBytes, l.PidsMax, fmtTS(time.Now()), siteID)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO site_limits (site_id, cpu_quota_pct, mem_limit_bytes, pids_max) VALUES (?, ?, ?, ?)`,
		siteID, l.CPUQuotaPct, l.MemLimitBytes, l.PidsMax); err != nil {
		return errx.Internal(err)
	}
	return nil
}
