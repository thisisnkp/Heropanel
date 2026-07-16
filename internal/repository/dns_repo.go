package repository

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// DNSStore implements dns.Repo over the datastore.
type DNSStore struct {
	db *DB
}

// NewDNSStore constructs a DNSStore.
func NewDNSStore(db *DB) *DNSStore { return &DNSStore{db: db} }

var _ dns.Repo = (*DNSStore)(nil)

const dnsZoneCols = `id, uid, owner_id, name, primary_ns, admin_email, serial, refresh, retry, expire, minimum, ttl, status, created_at, updated_at`

func (s *DNSStore) InsertZone(ctx context.Context, z *dns.ZoneRow) error {
	if z.UID == "" {
		z.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO dns_zones (uid, owner_id, name, primary_ns, admin_email, serial, refresh, retry, expire, minimum, ttl, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		z.UID, z.OwnerID, z.Name, z.PrimaryNS, z.AdminEmail, z.Serial, z.Refresh, z.Retry, z.Expire, z.Minimum, z.TTL, z.Status)
	if err != nil {
		return errx.Wrap(err, errx.KindConflict, "zone_exists", "A zone with that name already exists.")
	}
	if id, err := res.LastInsertId(); err == nil {
		z.ID = id
	}
	return nil
}

func (s *DNSStore) GetZoneByUID(ctx context.Context, uid string) (*dns.ZoneRow, error) {
	var z dns.ZoneRow
	err := s.db.GetContext(ctx, &z, `SELECT `+dnsZoneCols+` FROM dns_zones WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("zone_not_found", "No such zone.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &z, nil
}

func (s *DNSStore) GetZoneByID(ctx context.Context, id int64) (*dns.ZoneRow, error) {
	var z dns.ZoneRow
	err := s.db.GetContext(ctx, &z, `SELECT `+dnsZoneCols+` FROM dns_zones WHERE id = ?`, id)
	if isNoRows(err) {
		return nil, errx.NotFound("zone_not_found", "No such zone.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &z, nil
}

func (s *DNSStore) ListZones(ctx context.Context, ownerID int64, limit, offset int) ([]dns.ZoneRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []dns.ZoneRow
	var err error
	if ownerID > 0 {
		err = s.db.SelectContext(ctx, &rows,
			`SELECT `+dnsZoneCols+` FROM dns_zones WHERE owner_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`, ownerID, limit, offset)
	} else {
		err = s.db.SelectContext(ctx, &rows,
			`SELECT `+dnsZoneCols+` FROM dns_zones ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

func (s *DNSStore) ListActiveZones(ctx context.Context) ([]dns.ZoneRow, error) {
	var rows []dns.ZoneRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT `+dnsZoneCols+` FROM dns_zones WHERE status = 'active' ORDER BY id`); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

func (s *DNSStore) DeleteZone(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM dns_zones WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

func (s *DNSStore) SetSerial(ctx context.Context, zoneID, serial int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE dns_zones SET serial = ? WHERE id = ?`, serial, zoneID); err != nil {
		return errx.Internal(err)
	}
	return nil
}

const dnsRecordCols = `id, uid, zone_id, name, type, content, ttl, priority, created_at, updated_at`

func (s *DNSStore) InsertRecord(ctx context.Context, r *dns.RecordRow) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO dns_records (uid, zone_id, name, type, content, ttl, priority) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.ZoneID, r.Name, r.Type, r.Content, r.TTL, r.Priority)
	if err != nil {
		return errx.Internal(err)
	}
	if id, err := res.LastInsertId(); err == nil {
		r.ID = id
	}
	return nil
}

func (s *DNSStore) ListRecords(ctx context.Context, zoneID int64) ([]dns.RecordRow, error) {
	var rows []dns.RecordRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT `+dnsRecordCols+` FROM dns_records WHERE zone_id = ? ORDER BY id`, zoneID); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

func (s *DNSStore) GetRecordByUID(ctx context.Context, uid string) (*dns.RecordRow, error) {
	var r dns.RecordRow
	err := s.db.GetContext(ctx, &r, `SELECT `+dnsRecordCols+` FROM dns_records WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("record_not_found", "No such record.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &r, nil
}

func (s *DNSStore) DeleteRecord(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM dns_records WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}
