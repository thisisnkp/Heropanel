package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/nexpanel/internal/security"
	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
)

// FirewallStore implements security.FirewallRepo over the datastore.
type FirewallStore struct {
	db *DB
}

// NewFirewallStore constructs a FirewallStore.
func NewFirewallStore(db *DB) *FirewallStore { return &FirewallStore{db: db} }

var _ security.FirewallRepo = (*FirewallStore)(nil)

const fwRuleSelect = `SELECT id, uid, position, action, protocol, port, port_end, source, comment, created_at FROM firewall_rules`

// ListRules returns the ruleset in application order.
func (s *FirewallStore) ListRules(ctx context.Context) ([]security.RuleRecord, error) {
	var rows []security.RuleRecord
	if err := s.db.SelectContext(ctx, &rows, fwRuleSelect+` ORDER BY position ASC, id ASC`); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// InsertRule records a rule.
func (s *FirewallStore) InsertRule(ctx context.Context, r *security.RuleRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO firewall_rules (uid, position, action, protocol, port, port_end, source, comment, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.Position, r.Action, r.Protocol, r.Port, r.PortEnd, r.Source, r.Comment, fmtTS(time.Now()))
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// ListIPEntries returns the geo/IP allow-block entries.
func (s *FirewallStore) ListIPEntries(ctx context.Context) ([]security.IPListEntry, error) {
	var rows []security.IPListEntry
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT uid, cidr, mode, comment, country FROM firewall_iplist ORDER BY mode ASC, cidr ASC`); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// InsertIPEntry records an allow/block entry.
func (s *FirewallStore) InsertIPEntry(ctx context.Context, e *security.IPListEntry) error {
	if e.UID == "" {
		e.UID = idgen.NewULID()
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO firewall_iplist (uid, cidr, mode, comment, country, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		e.UID, e.CIDR, e.Mode, e.Comment, e.Country, fmtTS(time.Now())); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// InsertIPEntries bulk-inserts imported entries in one transaction so a
// country import is atomic (a mid-import failure rolls the whole set back).
func (s *FirewallStore) InsertIPEntries(ctx context.Context, entries []*security.IPListEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return errx.Internal(err)
	}
	defer func() { _ = tx.Rollback() }()
	now := fmtTS(time.Now())
	for _, e := range entries {
		if e.UID == "" {
			e.UID = idgen.NewULID()
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO firewall_iplist (uid, cidr, mode, comment, country, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			e.UID, e.CIDR, e.Mode, e.Comment, e.Country, now); err != nil {
			return errx.Internal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteIPEntriesByCountry removes every entry imported from a country.
func (s *FirewallStore) DeleteIPEntriesByCountry(ctx context.Context, country string) error {
	if country == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM firewall_iplist WHERE country = ?`, country); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteIPEntry removes an allow/block entry.
func (s *FirewallStore) DeleteIPEntry(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM firewall_iplist WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteRule removes a rule.
func (s *FirewallStore) DeleteRule(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM firewall_rules WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// NextPosition returns one past the highest position (append order).
func (s *FirewallStore) NextPosition(ctx context.Context) (int, error) {
	var max *int
	if err := s.db.GetContext(ctx, &max, `SELECT MAX(position) FROM firewall_rules`); err != nil {
		return 0, errx.Internal(err)
	}
	if max == nil {
		return 1, nil
	}
	return *max + 1, nil
}

// GetState returns the pending-apply state (the singleton row).
func (s *FirewallStore) GetState(ctx context.Context) (*security.State, error) {
	var st security.State
	err := s.db.GetContext(ctx, &st,
		`SELECT pending_token, deadline FROM firewall_state WHERE id = 1`)
	if isNoRows(err) {
		return &security.State{}, nil
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &st, nil
}

// SetState updates the pending-apply state.
func (s *FirewallStore) SetState(ctx context.Context, token, deadline string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE firewall_state SET pending_token = ?, deadline = ?, updated_at = ? WHERE id = 1`,
		token, deadline, fmtTS(time.Now())); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// MalwareStore implements security.MalwareRepo over the datastore.
type MalwareStore struct {
	db *DB
}

// NewMalwareStore constructs a MalwareStore.
func NewMalwareStore(db *DB) *MalwareStore { return &MalwareStore{db: db} }

var _ security.MalwareRepo = (*MalwareStore)(nil)

// InsertScan records a completed scan.
func (s *MalwareStore) InsertScan(ctx context.Context, r *security.ScanRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO malware_scans (uid, site_uid, target, infected_count, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.UID, r.SiteUID, r.Target, r.Infected, r.Status, fmtTS(time.Now()))
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// ListScans returns recent scans, newest first.
func (s *MalwareStore) ListScans(ctx context.Context, limit int) ([]security.ScanRecord, error) {
	var rows []security.ScanRecord
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT id, uid, site_uid, target, infected_count, status, created_at FROM malware_scans
		 ORDER BY created_at DESC, id DESC LIMIT ?`, limit); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

const quarantineSelect = `SELECT id, uid, site_uid, original_path, original_user, signature, status, created_at FROM malware_quarantine`

// InsertQuarantine records a quarantined item.
func (s *MalwareStore) InsertQuarantine(ctx context.Context, r *security.QuarantineRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO malware_quarantine (uid, site_uid, original_path, original_user, signature, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.SiteUID, r.OriginalPath, r.OriginalUser, r.Signature, r.Status, fmtTS(time.Now()))
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

// ListQuarantine returns quarantined items, newest first.
func (s *MalwareStore) ListQuarantine(ctx context.Context) ([]security.QuarantineRecord, error) {
	var rows []security.QuarantineRecord
	if err := s.db.SelectContext(ctx, &rows, quarantineSelect+` ORDER BY created_at DESC, id DESC`); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// GetQuarantine returns one item.
func (s *MalwareStore) GetQuarantine(ctx context.Context, uid string) (*security.QuarantineRecord, error) {
	var rec security.QuarantineRecord
	err := s.db.GetContext(ctx, &rec, quarantineSelect+` WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("not_found", "No such quarantined item.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

// SetQuarantineStatus flips an item's status (e.g. to restored).
func (s *MalwareStore) SetQuarantineStatus(ctx context.Context, uid, status string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE malware_quarantine SET status = ? WHERE uid = ?`, status, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// DeleteQuarantine removes an item's record.
func (s *MalwareStore) DeleteQuarantine(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM malware_quarantine WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}
