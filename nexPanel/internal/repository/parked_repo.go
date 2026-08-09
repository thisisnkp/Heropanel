package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/nexpanel/internal/domain"
	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
)

// ParkedDomainStore implements domain.ParkedRepo over the datastore.
type ParkedDomainStore struct {
	db *DB
}

// NewParkedDomainStore constructs a ParkedDomainStore.
func NewParkedDomainStore(db *DB) *ParkedDomainStore { return &ParkedDomainStore{db: db} }

var _ domain.ParkedRepo = (*ParkedDomainStore)(nil)

// parkedCols is the read projection. verified_at/site_id are nullable columns;
// COALESCE keeps the scan target simple Go types (string/int64, 0/"" meaning
// unset) instead of sql.Null* everywhere they are read.
const parkedCols = `id, uid, owner_id, fqdn, status, challenge_token,
	COALESCE(verified_at, '') AS verified_at, COALESCE(site_id, 0) AS site_id, created_at`

func (s *ParkedDomainStore) InsertParked(ctx context.Context, r *domain.ParkedRow) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	var siteID any
	if r.SiteID != 0 {
		siteID = r.SiteID
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO parked_domains (uid, owner_id, fqdn, status, challenge_token, site_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.UID, r.OwnerID, r.FQDN, r.Status, r.ChallengeToken, siteID)
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "domain_exists", "That domain is already parked or in use.")
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

func (s *ParkedDomainStore) GetParkedByUID(ctx context.Context, uid string) (*domain.ParkedRow, error) {
	var r domain.ParkedRow
	err := s.db.GetContext(ctx, &r, `SELECT `+parkedCols+` FROM parked_domains WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("domain_not_found", "No such parked domain.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &r, nil
}

func (s *ParkedDomainStore) GetParkedByFQDN(ctx context.Context, fqdn string) (*domain.ParkedRow, error) {
	var r domain.ParkedRow
	err := s.db.GetContext(ctx, &r, `SELECT `+parkedCols+` FROM parked_domains WHERE fqdn = ?`, fqdn)
	if isNoRows(err) {
		return nil, errx.NotFound("domain_not_found", "No such parked domain.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &r, nil
}

func (s *ParkedDomainStore) ListParked(ctx context.Context, ownerID int64) ([]domain.ParkedRow, error) {
	var rows []domain.ParkedRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT `+parkedCols+` FROM parked_domains WHERE owner_id = ? ORDER BY id DESC`, ownerID); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

func (s *ParkedDomainStore) SetParkedVerified(ctx context.Context, uid, verifiedAt string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE parked_domains SET status = ?, verified_at = ?, updated_at = ? WHERE uid = ?`,
		domain.ParkedVerified, verifiedAt, fmtTS(time.Now()), uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

func (s *ParkedDomainStore) AttachParked(ctx context.Context, uid string, siteID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE parked_domains SET site_id = ?, updated_at = ? WHERE uid = ?`,
		siteID, fmtTS(time.Now()), uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

func (s *ParkedDomainStore) DeleteParked(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM parked_domains WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ListVerifiedFQDNs / ListActiveZoneNames / ListAttachedFQDNs cross into the
// dns_zones/sites/domains tables directly — the repository layer already knows
// the whole schema (unlike the domain package, which stays decoupled from
// site/dns via interfaces), so this is the natural place for the union/anti-
// join queries the free-domain pool needs.

func (s *ParkedDomainStore) ListVerifiedFQDNs(ctx context.Context, ownerID int64) ([]string, error) {
	var out []string
	if err := s.db.SelectContext(ctx, &out,
		`SELECT fqdn FROM parked_domains WHERE owner_id = ? AND status = ?`,
		ownerID, domain.ParkedVerified); err != nil {
		return nil, errx.Internal(err)
	}
	return out, nil
}

func (s *ParkedDomainStore) ListActiveZoneNames(ctx context.Context, ownerID int64) ([]string, error) {
	var out []string
	if err := s.db.SelectContext(ctx, &out,
		`SELECT name FROM dns_zones WHERE owner_id = ? AND status = 'active'`, ownerID); err != nil {
		return nil, errx.Internal(err)
	}
	return out, nil
}

func (s *ParkedDomainStore) ListAttachedFQDNs(ctx context.Context, ownerID int64) ([]string, error) {
	var out []string
	if err := s.db.SelectContext(ctx, &out,
		`SELECT primary_domain FROM sites WHERE owner_id = ? AND deleted_at IS NULL
		 UNION
		 SELECT d.fqdn FROM domains d JOIN sites s ON s.id = d.site_id
		  WHERE s.owner_id = ? AND s.deleted_at IS NULL`,
		ownerID, ownerID); err != nil {
		return nil, errx.Internal(err)
	}
	return out, nil
}
