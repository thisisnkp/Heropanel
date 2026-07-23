package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/mail"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// MailStore implements mail.Repo over the datastore.
type MailStore struct {
	db *DB
}

// NewMailStore constructs a MailStore.
func NewMailStore(db *DB) *MailStore { return &MailStore{db: db} }

var _ mail.Repo = (*MailStore)(nil)

const mailDomainSelect = `SELECT id, uid, owner_id, domain, dkim_selector, dkim_private, dkim_public, status, created_at FROM mail_domains`

// InsertDomain records a mail domain.
func (s *MailStore) InsertDomain(ctx context.Context, r *mail.DomainRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO mail_domains (uid, owner_id, domain, dkim_selector, dkim_private, dkim_public, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.OwnerID, r.Domain, r.DKIMSelector, r.DKIMPrivate, r.DKIMPublic, r.Status, fmtTS(time.Now()))
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "domain_exists", "That mail domain already exists.")
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// ListDomains returns mail domains (ownerID 0 = all), newest first.
func (s *MailStore) ListDomains(ctx context.Context, ownerID int64) ([]mail.DomainRecord, error) {
	q, args := mailDomainSelect+` ORDER BY created_at DESC, id DESC`, []any{}
	if ownerID > 0 {
		q = mailDomainSelect + ` WHERE owner_id = ? ORDER BY created_at DESC, id DESC`
		args = append(args, ownerID)
	}
	var rows []mail.DomainRecord
	if err := s.db.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// GetDomainByUID returns one mail domain.
func (s *MailStore) GetDomainByUID(ctx context.Context, uid string) (*mail.DomainRecord, error) {
	var rec mail.DomainRecord
	err := s.db.GetContext(ctx, &rec, mailDomainSelect+` WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("domain_not_found", "No such mail domain.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

// UpdateDomainDKIM stores a domain's DKIM material (private key sealed).
func (s *MailStore) UpdateDomainDKIM(ctx context.Context, id int64, selector, sealedPrivate, public string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE mail_domains SET dkim_selector = ?, dkim_private = ?, dkim_public = ? WHERE id = ?`,
		selector, sealedPrivate, public, id); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteDomain removes a mail domain; accounts and aliases cascade.
func (s *MailStore) DeleteDomain(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mail_domains WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

const mailAccountSelect = `SELECT id, uid, domain_id, local_part, password_hash, quota_mb, status, created_at FROM mail_accounts`

// InsertAccount records a mailbox.
func (s *MailStore) InsertAccount(ctx context.Context, r *mail.AccountRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO mail_accounts (uid, domain_id, local_part, password_hash, quota_mb, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.DomainID, r.LocalPart, r.PasswordHash, r.QuotaMB, r.Status, fmtTS(time.Now()))
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "account_exists", "That mailbox already exists.")
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// ListAccounts returns a domain's mailboxes, stable order.
func (s *MailStore) ListAccounts(ctx context.Context, domainID int64) ([]mail.AccountRecord, error) {
	var rows []mail.AccountRecord
	if err := s.db.SelectContext(ctx, &rows,
		mailAccountSelect+` WHERE domain_id = ? ORDER BY local_part ASC`, domainID); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// GetAccountByUID returns one mailbox.
func (s *MailStore) GetAccountByUID(ctx context.Context, uid string) (*mail.AccountRecord, error) {
	var rec mail.AccountRecord
	err := s.db.GetContext(ctx, &rec, mailAccountSelect+` WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("account_not_found", "No such mailbox.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

// UpdateAccountPassword replaces a mailbox credential.
func (s *MailStore) UpdateAccountPassword(ctx context.Context, id int64, hash string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE mail_accounts SET password_hash = ? WHERE id = ?`, hash, id); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// UpdateAccountQuota changes a mailbox quota.
func (s *MailStore) UpdateAccountQuota(ctx context.Context, id int64, quotaMB int) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE mail_accounts SET quota_mb = ? WHERE id = ?`, quotaMB, id); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// UpdateAccountStatus flips active/suspended.
func (s *MailStore) UpdateAccountStatus(ctx context.Context, id int64, status string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE mail_accounts SET status = ? WHERE id = ?`, status, id); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteAccount removes a mailbox row.
func (s *MailStore) DeleteAccount(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mail_accounts WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// InsertAlias records an alias/forwarder pair.
func (s *MailStore) InsertAlias(ctx context.Context, r *mail.AliasRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO mail_aliases (uid, domain_id, source, destination, created_at) VALUES (?, ?, ?, ?, ?)`,
		r.UID, r.DomainID, r.Source, r.Destination, fmtTS(time.Now()))
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "alias_exists", "That alias already exists.")
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// ListAliases returns a domain's aliases, stable order.
func (s *MailStore) ListAliases(ctx context.Context, domainID int64) ([]mail.AliasRecord, error) {
	var rows []mail.AliasRecord
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT id, uid, domain_id, source, destination, created_at FROM mail_aliases
		 WHERE domain_id = ? ORDER BY source ASC, destination ASC`, domainID); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// GetAliasByUID returns one alias.
func (s *MailStore) GetAliasByUID(ctx context.Context, uid string) (*mail.AliasRecord, error) {
	var rec mail.AliasRecord
	err := s.db.GetContext(ctx, &rec,
		`SELECT id, uid, domain_id, source, destination, created_at FROM mail_aliases WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("alias_not_found", "No such alias.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

// DeleteAlias removes an alias row.
func (s *MailStore) DeleteAlias(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mail_aliases WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// RenderAccounts returns the complete account state joined with its domain,
// deterministically ordered — the renderers' input.
func (s *MailStore) RenderAccounts(ctx context.Context) ([]mail.RenderAccountRow, error) {
	var rows []struct {
		Domain       string `db:"domain"`
		LocalPart    string `db:"local_part"`
		PasswordHash string `db:"password_hash"`
		QuotaMB      int    `db:"quota_mb"`
		Status       string `db:"status"`
	}
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT d.domain AS domain, a.local_part AS local_part, a.password_hash AS password_hash,
		        a.quota_mb AS quota_mb, a.status AS status
		 FROM mail_accounts a JOIN mail_domains d ON d.id = a.domain_id
		 ORDER BY d.domain ASC, a.local_part ASC`); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]mail.RenderAccountRow, len(rows))
	for i, r := range rows {
		out[i] = mail.RenderAccountRow{
			Domain: r.Domain, LocalPart: r.LocalPart, PasswordHash: r.PasswordHash,
			QuotaMB: r.QuotaMB, Active: r.Status == "active",
		}
	}
	return out, nil
}

// RenderAliases returns the complete alias state joined with its domain.
func (s *MailStore) RenderAliases(ctx context.Context) ([]mail.RenderAliasRow, error) {
	var rows []struct {
		Domain      string `db:"domain"`
		Source      string `db:"source"`
		Destination string `db:"destination"`
	}
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT d.domain AS domain, l.source AS source, l.destination AS destination
		 FROM mail_aliases l JOIN mail_domains d ON d.id = l.domain_id
		 ORDER BY d.domain ASC, l.source ASC, l.destination ASC`); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]mail.RenderAliasRow, len(rows))
	for i, r := range rows {
		out[i] = mail.RenderAliasRow{Domain: r.Domain, Source: r.Source, Destination: r.Destination}
	}
	return out, nil
}

// AllDomainNames returns every mail domain name, sorted.
func (s *MailStore) AllDomainNames(ctx context.Context) ([]string, error) {
	var out []string
	if err := s.db.SelectContext(ctx, &out, `SELECT domain FROM mail_domains ORDER BY domain ASC`); err != nil {
		return nil, errx.Internal(err)
	}
	return out, nil
}
