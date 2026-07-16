package dns_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

type gwCall struct {
	capability string
	input      map[string]any
}

type mockGW struct{ calls []gwCall }

func (m *mockGW) Invoke(_ context.Context, capability string, input any) (map[string]any, error) {
	in, _ := input.(map[string]any)
	m.calls = append(m.calls, gwCall{capability: capability, input: in})
	return map[string]any{"ok": true}, nil
}
func (m *mockGW) Health(context.Context) error { return nil }

// in-memory dns.Repo
type fakeRepo struct {
	zones   map[string]*dns.ZoneRow // by uid
	byID    map[int64]*dns.ZoneRow
	records map[int64][]*dns.RecordRow // by zoneID
	recByU  map[string]*dns.RecordRow
	seq     int64
}

func newRepo() *fakeRepo {
	return &fakeRepo{zones: map[string]*dns.ZoneRow{}, byID: map[int64]*dns.ZoneRow{},
		records: map[int64][]*dns.RecordRow{}, recByU: map[string]*dns.RecordRow{}}
}

func (r *fakeRepo) InsertZone(_ context.Context, z *dns.ZoneRow) error {
	r.seq++
	z.ID = r.seq
	if z.UID == "" {
		z.UID = "z-" + z.Name
	}
	cp := *z
	r.zones[z.UID] = &cp
	r.byID[z.ID] = &cp
	return nil
}
func (r *fakeRepo) GetZoneByUID(_ context.Context, uid string) (*dns.ZoneRow, error) {
	if z, ok := r.zones[uid]; ok {
		cp := *z
		return &cp, nil
	}
	return nil, errx.NotFound("zone_not_found", "no zone")
}
func (r *fakeRepo) GetZoneByID(_ context.Context, id int64) (*dns.ZoneRow, error) {
	if z, ok := r.byID[id]; ok {
		cp := *z
		return &cp, nil
	}
	return nil, errx.NotFound("zone_not_found", "no zone")
}
func (r *fakeRepo) ListZones(_ context.Context, _ int64, _, _ int) ([]dns.ZoneRow, error) {
	var out []dns.ZoneRow
	for _, z := range r.zones {
		out = append(out, *z)
	}
	return out, nil
}
func (r *fakeRepo) ListActiveZones(_ context.Context) ([]dns.ZoneRow, error) {
	var out []dns.ZoneRow
	for _, z := range r.zones {
		out = append(out, *z)
	}
	return out, nil
}
func (r *fakeRepo) DeleteZone(_ context.Context, uid string) error {
	if z, ok := r.zones[uid]; ok {
		delete(r.byID, z.ID)
		delete(r.zones, uid)
	}
	return nil
}
func (r *fakeRepo) SetSerial(_ context.Context, zoneID, serial int64) error {
	if z, ok := r.byID[zoneID]; ok {
		z.Serial = serial
	}
	return nil
}
func (r *fakeRepo) InsertRecord(_ context.Context, rec *dns.RecordRow) error {
	r.seq++
	rec.ID = r.seq
	if rec.UID == "" {
		rec.UID = "r-" + rec.Name + rec.Type
	}
	cp := *rec
	r.records[rec.ZoneID] = append(r.records[rec.ZoneID], &cp)
	r.recByU[rec.UID] = &cp
	return nil
}
func (r *fakeRepo) ListRecords(_ context.Context, zoneID int64) ([]dns.RecordRow, error) {
	var out []dns.RecordRow
	for _, rec := range r.records[zoneID] {
		out = append(out, *rec)
	}
	return out, nil
}
func (r *fakeRepo) GetRecordByUID(_ context.Context, uid string) (*dns.RecordRow, error) {
	if rec, ok := r.recByU[uid]; ok {
		cp := *rec
		return &cp, nil
	}
	return nil, errx.NotFound("record_not_found", "no record")
}
func (r *fakeRepo) DeleteRecord(_ context.Context, uid string) error {
	if rec, ok := r.recByU[uid]; ok {
		list := r.records[rec.ZoneID]
		for i := range list {
			if list[i].UID == uid {
				r.records[rec.ZoneID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		delete(r.recByU, uid)
	}
	return nil
}

func lastCall(m *mockGW, cap string) *gwCall {
	for i := len(m.calls) - 1; i >= 0; i-- {
		if m.calls[i].capability == cap {
			return &m.calls[i]
		}
	}
	return nil
}

func TestCreateZoneAppliesToBind(t *testing.T) {
	gw := &mockGW{}
	svc := dns.NewService(newRepo(), gw)
	z, err := svc.CreateZone(context.Background(), dns.CreateZoneInput{OwnerID: 1, Name: "example.test", NSIP: "203.0.113.2"})
	if err != nil {
		t.Fatalf("create zone: %v", err)
	}
	if z.PrimaryNS != "ns1.example.test" || z.Serial == 0 {
		t.Fatalf("zone defaults wrong: %+v", z)
	}
	call := lastCall(gw, "dns.write_zone")
	if call == nil || call.input["zone"] != "example.test" {
		t.Fatalf("dns.write_zone not invoked: %+v", gw.calls)
	}
	zf, _ := call.input["zone_file"].(string)
	if !strings.Contains(zf, "SOA") || !strings.Contains(zf, "ns1.example.test.") {
		t.Fatalf("zone file not rendered: %q", zf)
	}
}

func TestCreateZoneValidates(t *testing.T) {
	svc := dns.NewService(newRepo(), &mockGW{})
	ctx := context.Background()
	if _, err := svc.CreateZone(ctx, dns.CreateZoneInput{OwnerID: 1, Name: "not a domain"}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation, got %v", err)
	}
	// An in-zone nameserver without a glue IP is rejected (BIND would not load it).
	if _, err := svc.CreateZone(ctx, dns.CreateZoneInput{OwnerID: 1, Name: "example.test"}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want ns_ip_required validation, got %v", err)
	}
}

func TestAddRecordBumpsSerialAndReapplies(t *testing.T) {
	gw := &mockGW{}
	repo := newRepo()
	svc := dns.NewService(repo, gw)
	ctx := context.Background()
	z, err := svc.CreateZone(ctx, dns.CreateZoneInput{OwnerID: 1, Name: "example.test", NSIP: "203.0.113.2"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	serial0 := z.Serial

	if _, err := svc.AddRecord(ctx, z.UID, dns.AddRecordInput{Name: "www", Type: "A", Content: "203.0.113.5"}); err != nil {
		t.Fatalf("add record: %v", err)
	}
	// The zone was re-applied with the new record and a higher serial.
	call := lastCall(gw, "dns.write_zone")
	zf, _ := call.input["zone_file"].(string)
	if !strings.Contains(zf, "203.0.113.5") {
		t.Fatalf("record not in re-rendered zone: %q", zf)
	}
	updated, _ := svc.GetZone(ctx, z.UID)
	if updated.Serial <= serial0 {
		t.Fatalf("serial not bumped: %d -> %d", serial0, updated.Serial)
	}

	// A bad record is rejected before any apply.
	before := len(gw.calls)
	if _, err := svc.AddRecord(ctx, z.UID, dns.AddRecordInput{Name: "bad", Type: "A", Content: "not-an-ip"}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for bad A content, got %v", err)
	}
	if len(gw.calls) != before {
		t.Fatal("no broker call should happen for an invalid record")
	}
}

func TestDeleteZoneRemovesFromBind(t *testing.T) {
	gw := &mockGW{}
	svc := dns.NewService(newRepo(), gw)
	ctx := context.Background()
	z, _ := svc.CreateZone(ctx, dns.CreateZoneInput{OwnerID: 1, Name: "example.test", NSIP: "203.0.113.2"})
	if err := svc.DeleteZone(ctx, z.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if lastCall(gw, "dns.remove_zone") == nil {
		t.Fatalf("dns.remove_zone not invoked: %+v", gw.calls)
	}
}
