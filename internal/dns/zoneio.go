package dns

import (
	"context"
	"strconv"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Zone import/export moves a whole zone's records in and out as a standard
// RFC 1035 master file — the format every DNS provider and `dig AXFR` speaks, so
// a zone can be migrated onto (or off) the panel without hand-entering records.
//
// Export is just the rendered zone file (RenderZoneFile), which is already a
// valid master file. Import is the interesting half: a tolerant parser for the
// common master-file shapes, deliberately scoped to the record types the panel
// manages. The SOA and the apex NS are skipped on import — the panel owns those
// from the zone's own settings; importing a foreign SOA/serial would fight the
// panel's serial bookkeeping.

// ParsedRecord is one record pulled from a master file, already relative to the
// zone and ready to validate/insert.
type ParsedRecord struct {
	Name     string
	Type     string
	Content  string
	TTL      int
	Priority int
}

// ParseZoneFile parses a master file into records relative to zone. It honors
// $ORIGIN and $TTL, owner-name inheritance (a line starting with whitespace
// reuses the previous owner), an optional TTL and IN class, and the record types
// the panel supports. SOA and the apex NS/the zone's own glue are left to the
// panel. Unsupported types and un-parseable lines are collected as skips rather
// than failing the whole import, so one odd record does not block a migration.
func ParseZoneFile(zone, text string) (records []ParsedRecord, skipped []string, err error) {
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	origin := zone
	defaultTTL := 3600
	lastOwner := "@"

	for _, rawLine := range strings.Split(text, "\n") {
		line := stripComment(rawLine)
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Directives.
		if strings.HasPrefix(strings.TrimSpace(line), "$") {
			fields := strings.Fields(line)
			switch strings.ToUpper(fields[0]) {
			case "$ORIGIN":
				if len(fields) >= 2 {
					origin = strings.ToLower(strings.TrimSuffix(fields[1], "."))
				}
			case "$TTL":
				if len(fields) >= 2 {
					if n, e := strconv.Atoi(fields[1]); e == nil {
						defaultTTL = n
					}
				}
			}
			continue
		}
		// Multi-line records (SOA with parentheses) are folded onto one logical
		// line elsewhere; a bare "(" or ")" line is ignored here.
		startsIndented := line[0] == ' ' || line[0] == '\t'
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		owner := lastOwner
		if !startsIndented {
			owner = fields[0]
			fields = fields[1:]
		}
		lastOwner = owner

		// Optional TTL, optional class (IN), then the type.
		ttl := defaultTTL
		if len(fields) > 0 && isAllDigitsStr(fields[0]) {
			ttl, _ = strconv.Atoi(fields[0])
			fields = fields[1:]
		}
		if len(fields) > 0 && strings.EqualFold(fields[0], "IN") {
			fields = fields[1:]
		}
		if len(fields) < 2 {
			skipped = append(skipped, strings.TrimSpace(rawLine))
			continue
		}
		rtype := strings.ToUpper(fields[0])
		rdata := strings.TrimSpace(strings.Join(fields[1:], " "))

		if rtype == "SOA" {
			continue // the panel owns the SOA
		}
		if !supportedTypes[rtype] {
			skipped = append(skipped, strings.TrimSpace(rawLine))
			continue
		}

		label := ownerToLabel(owner, origin, zone)
		// Skip the apex NS records — the panel manages the zone's own nameservers.
		if rtype == "NS" && label == "@" {
			continue
		}

		pr := ParsedRecord{Name: label, Type: rtype, Content: rdata, TTL: ttl}
		if rtype == "MX" || rtype == "SRV" {
			// Priority is the first rdata token for MX; for SRV it is also first.
			parts := strings.Fields(rdata)
			if len(parts) >= 2 {
				if p, e := strconv.Atoi(parts[0]); e == nil {
					pr.Priority = p
					if rtype == "MX" {
						pr.Content = strings.Join(parts[1:], " ")
					}
				}
			}
		}
		if rtype == "TXT" {
			pr.Content = unquoteTXT(rdata)
		}
		records = append(records, pr)
	}
	return records, skipped, nil
}

// ownerToLabel converts a master-file owner name into the panel's label form
// (relative to the zone, or "@" for the apex).
func ownerToLabel(owner, origin, zone string) string {
	o := strings.ToLower(strings.TrimSpace(owner))
	if o == "@" || o == "" {
		return "@"
	}
	if strings.HasSuffix(o, ".") {
		// Fully qualified: strip the zone suffix.
		fq := strings.TrimSuffix(o, ".")
		if fq == zone {
			return "@"
		}
		if strings.HasSuffix(fq, "."+zone) {
			return strings.TrimSuffix(fq, "."+zone)
		}
		return fq // outside the zone; keep as-is (validation may reject)
	}
	// Relative to $ORIGIN. When origin differs from the zone apex, qualify it.
	if origin != "" && origin != zone {
		full := o + "." + origin
		if full == zone {
			return "@"
		}
		if strings.HasSuffix(full, "."+zone) {
			return strings.TrimSuffix(full, "."+zone)
		}
	}
	return o
}

// stripComment removes an unquoted ";" comment from a master-file line.
func stripComment(line string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case ';':
			if !inQuote {
				return line[:i]
			}
		}
	}
	return line
}

// unquoteTXT joins the character-strings of a TXT rdata into the raw value.
func unquoteTXT(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "\"") {
		return s
	}
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isAllDigitsStr(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ExportZone renders a zone's full master file (SOA + NS + all records).
func (s *Service) ExportZone(ctx context.Context, zoneUID string) (string, error) {
	z, err := s.repo.GetZoneByUID(ctx, zoneUID)
	if err != nil {
		return "", err
	}
	records, err := s.repo.ListRecords(ctx, z.ID)
	if err != nil {
		return "", err
	}
	return RenderZoneFile(z, records), nil
}

// ImportResult reports what an import did.
type ImportResult struct {
	Imported int      `json:"imported"`
	Skipped  []string `json:"skipped"`
}

// ImportZone parses a master file and adds its records to the zone (additive —
// it does not delete existing records), then reapplies the zone once. Each record
// is validated with the same rules the API uses; a record that fails validation
// is skipped and reported rather than aborting the whole import.
func (s *Service) ImportZone(ctx context.Context, zoneUID, text string) (*ImportResult, error) {
	z, err := s.repo.GetZoneByUID(ctx, zoneUID)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	parsed, skipped, err := ParseZoneFile(z.Name, text)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{Skipped: skipped}
	for _, p := range parsed {
		in := AddRecordInput{Name: p.Name, Type: p.Type, Content: p.Content, TTL: p.TTL, Priority: p.Priority}
		if err := validateRecord(&in); err != nil {
			res.Skipped = append(res.Skipped, p.Name+" "+p.Type+" "+p.Content)
			continue
		}
		if err := s.repo.InsertRecord(ctx, &RecordRow{
			ZoneID: z.ID, Name: in.Name, Type: in.Type, Content: in.Content, TTL: in.TTL, Priority: in.Priority,
		}); err != nil {
			return nil, err
		}
		res.Imported++
	}
	if res.Imported == 0 {
		return res, nil
	}
	if err := s.reapply(ctx, z); err != nil {
		return nil, errx.Wrap(err, errx.KindUpstream, "import_apply_failed", "Records were imported but BIND rejected the zone.")
	}
	return res, nil
}
