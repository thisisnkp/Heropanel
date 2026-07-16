package repository

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/ssl"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// CertStore implements ssl.Repo over the datastore.
type CertStore struct{ db *DB }

// NewCertStore constructs a CertStore.
func NewCertStore(db *DB) *CertStore { return &CertStore{db: db} }

var _ ssl.Repo = (*CertStore)(nil)

const certCols = `id, uid, owner_id, provider, common_name, sans, is_wildcard, cert_pem, privkey_enc,
	COALESCE(issued_at,'') AS issued_at, COALESCE(expires_at,'') AS expires_at, auto_renew, status, created_at, webroot`

// ListDueForRenewal implements ssl.Repo: auto-renewing, currently-valid certs
// expiring at or before `before`. Custom uploads are excluded — HeroPanel cannot
// re-obtain someone else's certificate.
func (s *CertStore) ListDueForRenewal(ctx context.Context, before string) ([]ssl.Record, error) {
	var recs []ssl.Record
	if err := s.db.SelectContext(ctx, &recs,
		`SELECT `+certCols+` FROM ssl_certificates
		 WHERE auto_renew = 1 AND status = 'valid' AND provider <> 'custom'
		   AND expires_at IS NOT NULL AND expires_at <> '' AND expires_at <= ?
		 ORDER BY expires_at`, before); err != nil {
		return nil, errx.Internal(err)
	}
	return recs, nil
}

func (s *CertStore) Insert(ctx context.Context, r *ssl.Record) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	// Upsert by common_name (re-issuing — including a renewal — replaces the record).
	res, err := s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET provider=?, sans=?, cert_pem=?, privkey_enc=?, issued_at=?, expires_at=?, status=?, webroot=?, is_wildcard=?
		  WHERE common_name = ?`,
		r.Provider, r.SANs, r.CertPEM, r.PrivkeyEnc, r.IssuedAt, r.ExpiresAt, r.Status, r.Webroot, r.IsWildcard, r.CommonName)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	ins, err := s.db.ExecContext(ctx,
		`INSERT INTO ssl_certificates (uid, owner_id, provider, common_name, sans, cert_pem, privkey_enc, issued_at, expires_at, auto_renew, status, webroot, is_wildcard)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.OwnerID, r.Provider, r.CommonName, r.SANs, r.CertPEM, r.PrivkeyEnc, r.IssuedAt, r.ExpiresAt, r.AutoRenew, r.Status, r.Webroot, r.IsWildcard)
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "cert_exists", "A certificate for that name already exists.")
	}
	if id, err := ins.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

func (s *CertStore) List(ctx context.Context, ownerID int64, limit, offset int) ([]ssl.Record, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var recs []ssl.Record
	var err error
	if ownerID > 0 {
		err = s.db.SelectContext(ctx, &recs,
			`SELECT `+certCols+` FROM ssl_certificates WHERE owner_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, ownerID, limit, offset)
	} else {
		err = s.db.SelectContext(ctx, &recs,
			`SELECT `+certCols+` FROM ssl_certificates ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return recs, nil
}

func (s *CertStore) GetByUID(ctx context.Context, uid string) (*ssl.Record, error) {
	var rec ssl.Record
	err := s.db.GetContext(ctx, &rec, `SELECT `+certCols+` FROM ssl_certificates WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("cert_not_found", "No such certificate.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

func (s *CertStore) Delete(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM ssl_certificates WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}
