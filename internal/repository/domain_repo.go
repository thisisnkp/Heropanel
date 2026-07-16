package repository

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// DomainStore implements domain.Repo over the datastore.
type DomainStore struct {
	db *DB
}

// NewDomainStore constructs a DomainStore.
func NewDomainStore(db *DB) *DomainStore { return &DomainStore{db: db} }

var _ domain.Repo = (*DomainStore)(nil)

const domainCols = `id, uid, site_id, fqdn, kind, force_https, redirect_to, redirect_code, created_at`

func (s *DomainStore) Insert(ctx context.Context, r *domain.Row) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO domains (uid, site_id, fqdn, kind, force_https, redirect_to, redirect_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.SiteID, r.FQDN, r.Kind, r.ForceHTTPS, r.RedirectTo, r.RedirectCode)
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "domain_exists", "That domain is already in use.")
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

func (s *DomainStore) ListBySiteID(ctx context.Context, siteID int64) ([]domain.Row, error) {
	var rows []domain.Row
	// Primary first, then the rest by id — a stable order keeps the rendered
	// config byte-identical between applies.
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT `+domainCols+` FROM domains WHERE site_id = ?
		 ORDER BY CASE kind WHEN 'primary' THEN 0 ELSE 1 END, id`, siteID); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

func (s *DomainStore) GetByUID(ctx context.Context, uid string) (*domain.Row, error) {
	var r domain.Row
	err := s.db.GetContext(ctx, &r, `SELECT `+domainCols+` FROM domains WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("domain_not_found", "No such domain.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &r, nil
}

func (s *DomainStore) Delete(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

func (s *DomainStore) SetForceHTTPSForSite(ctx context.Context, siteID int64, on bool) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE domains SET force_https = ? WHERE site_id = ?`, on, siteID); err != nil {
		return errx.Internal(err)
	}
	return nil
}
