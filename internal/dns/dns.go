// Package dns implements authoritative DNS management: HeroPanel hosts zones and
// records and serves them from BIND9. hpd renders the zone file and the zone
// declaration from DB state; the privileged broker writes them, validates with
// named-checkzone, and reloads BIND (same render → broker → reload shape as the
// web-server and php-fpm flows). See docs/13-dns.md.
package dns

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// zonesDir is where BIND zone files live; both hpd (which renders the file path
// into named.conf) and the broker (which writes them) use this constant.
const zonesDir = "/etc/bind/zones"

// ZoneFilePath is the on-disk path of a zone's records file.
func ZoneFilePath(zone string) string { return zonesDir + "/db." + zone }

// Supported record types.
var supportedTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "MX": true,
	"TXT": true, "NS": true, "SRV": true, "CAA": true,
}

// Zone is the API view of an authoritative zone.
type Zone struct {
	UID        string `json:"uid"`
	Name       string `json:"name"`
	PrimaryNS  string `json:"primary_ns"`
	AdminEmail string `json:"admin_email"`
	Serial     int64  `json:"serial"`
	TTL        int    `json:"ttl"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// Record is the API view of a DNS record.
type Record struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	TTL       int    `json:"ttl"`
	Priority  int    `json:"priority"`
	CreatedAt string `json:"created_at"`
}

// ZoneRow / RecordRow are the persistence rows.
type ZoneRow struct {
	ID         int64  `db:"id"`
	UID        string `db:"uid"`
	OwnerID    int64  `db:"owner_id"`
	Name       string `db:"name"`
	PrimaryNS  string `db:"primary_ns"`
	AdminEmail string `db:"admin_email"`
	Serial     int64  `db:"serial"`
	Refresh    int    `db:"refresh"`
	Retry      int    `db:"retry"`
	Expire     int    `db:"expire"`
	Minimum    int    `db:"minimum"`
	TTL        int    `db:"ttl"`
	Status     string `db:"status"`
	CreatedAt  string `db:"created_at"`
	UpdatedAt  string `db:"updated_at"`
}

type RecordRow struct {
	ID        int64  `db:"id"`
	UID       string `db:"uid"`
	ZoneID    int64  `db:"zone_id"`
	Name      string `db:"name"`
	Type      string `db:"type"`
	Content   string `db:"content"`
	TTL       int    `db:"ttl"`
	Priority  int    `db:"priority"`
	CreatedAt string `db:"created_at"`
	UpdatedAt string `db:"updated_at"`
}

// Repo is the persistence contract (implemented by internal/repository).
type Repo interface {
	InsertZone(ctx context.Context, z *ZoneRow) error
	GetZoneByUID(ctx context.Context, uid string) (*ZoneRow, error)
	GetZoneByID(ctx context.Context, id int64) (*ZoneRow, error)
	ListZones(ctx context.Context, ownerID int64, limit, offset int) ([]ZoneRow, error)
	ListActiveZones(ctx context.Context) ([]ZoneRow, error)
	DeleteZone(ctx context.Context, uid string) error
	SetSerial(ctx context.Context, zoneID, serial int64) error
	InsertRecord(ctx context.Context, r *RecordRow) error
	ListRecords(ctx context.Context, zoneID int64) ([]RecordRow, error)
	GetRecordByUID(ctx context.Context, uid string) (*RecordRow, error)
	DeleteRecord(ctx context.Context, uid string) error
}

// CreateZoneInput / AddRecordInput are the request shapes.
type CreateZoneInput struct {
	OwnerID    int64
	Name       string
	PrimaryNS  string
	AdminEmail string
	// NSIP is the IPv4 glue address for the primary nameserver. Required when the
	// primary NS is inside this zone (BIND refuses to load an in-zone NS with no
	// address record), ignored otherwise.
	NSIP string
}

// nsInZone reports whether the nameserver is within the zone (and therefore
// needs a glue A record in the zone).
func nsInZone(ns, zone string) bool {
	return ns == zone || strings.HasSuffix(ns, "."+zone)
}

// nsGlueLabel is the record label for an in-zone nameserver's glue A record.
func nsGlueLabel(ns, zone string) string {
	if ns == zone {
		return "@"
	}
	return strings.TrimSuffix(ns, "."+zone)
}

type AddRecordInput struct {
	Name     string
	Type     string
	Content  string
	TTL      int
	Priority int
}

// ── validation ───────────────────────────────────────────────────────────────

var (
	reHost   = regexp.MustCompile(`^(\*\.)?([a-zA-Z0-9_]([a-zA-Z0-9_-]{0,61}[a-zA-Z0-9_])?\.)+[a-zA-Z]{2,63}\.?$`)
	reName   = regexp.MustCompile(`^(@|\*|[a-zA-Z0-9_]([a-zA-Z0-9_.-]{0,251}[a-zA-Z0-9_])?)$`)
	reTarget = regexp.MustCompile(`^([a-zA-Z0-9_]([a-zA-Z0-9_-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9_]([a-zA-Z0-9_-]{0,61}[a-zA-Z0-9])?\.?$`)
)

func validateZoneInput(in *CreateZoneInput) error {
	in.Name = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(in.Name, ".")))
	in.PrimaryNS = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(in.PrimaryNS, ".")))
	in.AdminEmail = strings.TrimSpace(in.AdminEmail)
	if !reHost.MatchString(in.Name) {
		return errx.Validation("invalid_zone", "A valid zone name (FQDN) is required.",
			errx.Field{Field: "name", Code: "invalid", Message: "invalid zone"})
	}
	if in.PrimaryNS == "" {
		in.PrimaryNS = "ns1." + in.Name
	}
	if !reHost.MatchString(in.PrimaryNS) {
		return errx.Validation("invalid_primary_ns", "Primary nameserver must be a hostname.")
	}
	if in.AdminEmail == "" {
		in.AdminEmail = "hostmaster@" + in.Name
	}
	if at := strings.LastIndex(in.AdminEmail, "@"); at <= 0 || at == len(in.AdminEmail)-1 {
		return errx.Validation("invalid_admin_email", "A valid admin email is required.")
	}
	// An in-zone nameserver needs glue: require a valid IPv4 for it.
	in.NSIP = strings.TrimSpace(in.NSIP)
	if nsInZone(in.PrimaryNS, in.Name) {
		ip := net.ParseIP(in.NSIP)
		if in.NSIP == "" {
			return errx.Validation("ns_ip_required",
				"The primary nameserver is inside this zone, so its IPv4 glue address (ns_ip) is required.",
				errx.Field{Field: "ns_ip", Code: "required", Message: "required for in-zone NS"})
		}
		if ip == nil || ip.To4() == nil {
			return errx.Validation("invalid_ns_ip", "ns_ip must be a valid IPv4 address.")
		}
	}
	return nil
}

func validateRecord(in *AddRecordInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
	in.Content = strings.TrimSpace(in.Content)
	if in.Name == "" {
		in.Name = "@"
	}
	if !reName.MatchString(in.Name) {
		return errx.Validation("invalid_record_name", "Invalid record name.",
			errx.Field{Field: "name", Code: "invalid", Message: "invalid name"})
	}
	if !supportedTypes[in.Type] {
		return errx.Validation("invalid_record_type", "Unsupported record type.",
			errx.Field{Field: "type", Code: "unsupported", Message: in.Type})
	}
	if in.TTL == 0 {
		in.TTL = 3600
	}
	if in.TTL < 60 || in.TTL > 604800 {
		return errx.Validation("invalid_ttl", "TTL must be between 60 and 604800 seconds.")
	}
	if strings.ContainsAny(in.Content, "\n\r\x00") || in.Content == "" {
		return errx.Validation("invalid_content", "Record content is required and must be single-line.")
	}
	if err := validateContent(in.Type, in.Content); err != nil {
		return err
	}
	if (in.Type == "MX" || in.Type == "SRV") && (in.Priority < 0 || in.Priority > 65535) {
		return errx.Validation("invalid_priority", "Priority must be between 0 and 65535.")
	}
	return nil
}

func validateContent(typ, content string) error {
	bad := func() error { return errx.Validation("invalid_content", "Invalid content for a "+typ+" record.") }
	switch typ {
	case "A":
		ip := net.ParseIP(content)
		if ip == nil || ip.To4() == nil {
			return bad()
		}
	case "AAAA":
		ip := net.ParseIP(content)
		if ip == nil || ip.To4() != nil {
			return bad()
		}
	case "CNAME", "NS", "MX":
		if !reTarget.MatchString(content) {
			return bad()
		}
	case "TXT", "CAA", "SRV":
		if len(content) > 2000 {
			return bad()
		}
	}
	return nil
}

// ── rendering ────────────────────────────────────────────────────────────────

// RenderZoneFile renders a BIND zone file for a zone and its records.
func RenderZoneFile(z *ZoneRow, records []RecordRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$TTL %d\n", z.TTL)
	fmt.Fprintf(&b, "@\tIN\tSOA\t%s. %s. (\n", z.PrimaryNS, rname(z.AdminEmail))
	fmt.Fprintf(&b, "\t%d %d %d %d %d )\n", z.Serial, z.Refresh, z.Retry, z.Expire, z.Minimum)
	fmt.Fprintf(&b, "@\tIN\tNS\t%s.\n", z.PrimaryNS)
	for i := range records {
		b.WriteString(renderRecord(&records[i]))
	}
	return b.String()
}

func renderRecord(r *RecordRow) string {
	rdata := r.Content
	switch r.Type {
	case "TXT":
		if !strings.HasPrefix(rdata, "\"") {
			rdata = quoteTXT(rdata)
		}
	case "MX", "SRV":
		rdata = strconv.Itoa(r.Priority) + " " + rdata
	}
	return fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", r.Name, r.TTL, r.Type, rdata)
}

// quoteTXT quotes a TXT value for a zone file, splitting into 255-byte
// character-strings — BIND's per-string limit. A DKIM public key (~400 bytes)
// is the everyday case that overflows a single string; multiple quoted
// strings on one record are the RFC 1035 way to carry it, and resolvers
// concatenate them.
func quoteTXT(s string) string {
	const max = 255
	if len(s) <= max {
		return strconv.Quote(s)
	}
	var parts []string
	for len(s) > 0 {
		n := max
		if len(s) < n {
			n = len(s)
		}
		parts = append(parts, strconv.Quote(s[:n]))
		s = s[n:]
	}
	return strings.Join(parts, " ")
}

// RenderNamedConf renders the declarative set of zone {} blocks for all active
// zones (BIND's named.conf.heropanel include).
func RenderNamedConf(zones []ZoneRow) string {
	var b strings.Builder
	for i := range zones {
		fmt.Fprintf(&b, "zone \"%s\" {\n    type master;\n    file \"%s\";\n};\n",
			zones[i].Name, ZoneFilePath(zones[i].Name))
	}
	return b.String()
}

// rname converts an admin email (admin@example.test) to the SOA RNAME form
// (admin.example.test), escaping any dots in the local part.
func rname(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email
	}
	local := strings.ReplaceAll(email[:at], ".", "\\.")
	return local + "." + email[at+1:]
}
