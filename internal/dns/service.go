package dns

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Zone SOA defaults (seconds).
const (
	defRefresh = 3600
	defRetry   = 900
	defExpire  = 1209600
	defMinimum = 300
	defTTL     = 3600
)

// Service manages zones and records and applies them to BIND via the broker.
type Service struct {
	repo   Repo
	broker broker.Gateway
}

// NewService constructs the DNS Service. broker may be nil (writes then report
// "unavailable"; reads still work).
func NewService(repo Repo, gw broker.Gateway) *Service { return &Service{repo: repo, broker: gw} }

func (s *Service) requireBroker() error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable", "The broker is not available; DNS cannot be applied.")
	}
	return nil
}

// CreateZone creates an authoritative zone (SOA + primary NS) and loads it.
func (s *Service) CreateZone(ctx context.Context, in CreateZoneInput) (*Zone, error) {
	if err := validateZoneInput(&in); err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	z := &ZoneRow{
		OwnerID: in.OwnerID, Name: in.Name, PrimaryNS: in.PrimaryNS, AdminEmail: in.AdminEmail,
		Serial: nextSerial(0), Refresh: defRefresh, Retry: defRetry, Expire: defExpire,
		Minimum: defMinimum, TTL: defTTL, Status: "active",
	}
	if err := s.repo.InsertZone(ctx, z); err != nil {
		return nil, err
	}
	// Seed the glue A record for an in-zone nameserver, so BIND can load the zone.
	if nsInZone(z.PrimaryNS, z.Name) {
		if err := s.repo.InsertRecord(ctx, &RecordRow{
			ZoneID: z.ID, Name: nsGlueLabel(z.PrimaryNS, z.Name), Type: "A", Content: in.NSIP, TTL: z.TTL,
		}); err != nil {
			_ = s.repo.DeleteZone(ctx, z.UID)
			return nil, err
		}
	}
	if err := s.apply(ctx, z); err != nil {
		_ = s.repo.DeleteZone(ctx, z.UID)
		return nil, err
	}
	return toZoneView(z), nil
}

// GetZone returns a zone by UID.
func (s *Service) GetZone(ctx context.Context, uid string) (*Zone, error) {
	z, err := s.repo.GetZoneByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	return toZoneView(z), nil
}

// ListZones lists zones (ownerID 0 = all).
func (s *Service) ListZones(ctx context.Context, ownerID int64, limit, offset int) ([]Zone, error) {
	rows, err := s.repo.ListZones(ctx, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]Zone, len(rows))
	for i := range rows {
		out[i] = *toZoneView(&rows[i])
	}
	return out, nil
}

// DeleteZone removes a zone (and its records) and drops it from BIND.
func (s *Service) DeleteZone(ctx context.Context, uid string) error {
	z, err := s.repo.GetZoneByUID(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	if err := s.repo.DeleteZone(ctx, uid); err != nil {
		return err
	}
	active, err := s.repo.ListActiveZones(ctx)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "dns.remove_zone", map[string]any{
		"zone":       z.Name,
		"named_conf": RenderNamedConf(active),
	})
	return err
}

// ListRecords lists a zone's records.
func (s *Service) ListRecords(ctx context.Context, zoneUID string) ([]Record, error) {
	z, err := s.repo.GetZoneByUID(ctx, zoneUID)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListRecords(ctx, z.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Record, len(rows))
	for i := range rows {
		out[i] = *toRecordView(&rows[i])
	}
	return out, nil
}

// AddRecord adds a record to a zone, bumps the serial, and reloads BIND.
func (s *Service) AddRecord(ctx context.Context, zoneUID string, in AddRecordInput) (*Record, error) {
	if err := validateRecord(&in); err != nil {
		return nil, err
	}
	z, err := s.repo.GetZoneByUID(ctx, zoneUID)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	r := &RecordRow{
		ZoneID: z.ID, Name: in.Name, Type: in.Type, Content: in.Content,
		TTL: in.TTL, Priority: in.Priority,
	}
	if err := s.repo.InsertRecord(ctx, r); err != nil {
		return nil, err
	}
	if err := s.reapply(ctx, z); err != nil {
		return nil, err
	}
	return toRecordView(r), nil
}

// DeleteRecord removes a record, bumps the serial, and reloads BIND.
func (s *Service) DeleteRecord(ctx context.Context, recordUID string) error {
	r, err := s.repo.GetRecordByUID(ctx, recordUID)
	if err != nil {
		return err
	}
	z, err := s.repo.GetZoneByID(ctx, r.ZoneID)
	if err != nil {
		return err
	}
	if err := s.requireBroker(); err != nil {
		return err
	}
	if err := s.repo.DeleteRecord(ctx, recordUID); err != nil {
		return err
	}
	return s.reapply(ctx, z)
}

// reapply bumps the zone serial and re-renders + applies the zone.
func (s *Service) reapply(ctx context.Context, z *ZoneRow) error {
	z.Serial = nextSerial(z.Serial)
	if err := s.repo.SetSerial(ctx, z.ID, z.Serial); err != nil {
		return err
	}
	return s.apply(ctx, z)
}

// apply renders the zone file + the declarative named.conf and writes them via
// the broker (which validates with named-checkzone and reloads).
func (s *Service) apply(ctx context.Context, z *ZoneRow) error {
	records, err := s.repo.ListRecords(ctx, z.ID)
	if err != nil {
		return err
	}
	active, err := s.repo.ListActiveZones(ctx)
	if err != nil {
		return err
	}
	_, err = s.broker.Invoke(ctx, "dns.write_zone", map[string]any{
		"zone":       z.Name,
		"zone_file":  RenderZoneFile(z, records),
		"named_conf": RenderNamedConf(active),
	})
	return err
}

// nextSerial returns a date-based, strictly increasing SOA serial.
func nextSerial(prev int64) int64 {
	y, m, d := time.Now().UTC().Date()
	base := int64(y)*1000000 + int64(m)*10000 + int64(d)*100
	if prev >= base {
		return prev + 1
	}
	return base + 1
}

func toZoneView(z *ZoneRow) *Zone {
	return &Zone{
		UID: z.UID, Name: z.Name, PrimaryNS: z.PrimaryNS, AdminEmail: z.AdminEmail,
		Serial: z.Serial, TTL: z.TTL, Status: z.Status, CreatedAt: z.CreatedAt, UpdatedAt: z.UpdatedAt,
	}
}

func toRecordView(r *RecordRow) *Record {
	return &Record{
		UID: r.UID, Name: r.Name, Type: r.Type, Content: r.Content,
		TTL: r.TTL, Priority: r.Priority, CreatedAt: r.CreatedAt,
	}
}
