package dns_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/dns"
)

func TestRenderZoneFile(t *testing.T) {
	z := &dns.ZoneRow{
		Name: "example.test", PrimaryNS: "ns1.example.test", AdminEmail: "admin@example.test",
		Serial: 2026071501, Refresh: 3600, Retry: 900, Expire: 1209600, Minimum: 300, TTL: 3600,
	}
	recs := []dns.RecordRow{
		{Name: "@", Type: "A", Content: "203.0.113.10", TTL: 3600},
		{Name: "www", Type: "CNAME", Content: "@", TTL: 3600},
		{Name: "@", Type: "MX", Content: "mail.example.test.", TTL: 3600, Priority: 10},
		{Name: "_dmarc", Type: "TXT", Content: "v=DMARC1; p=none", TTL: 3600},
	}
	out := dns.RenderZoneFile(z, recs)
	for _, want := range []string{
		"$TTL 3600",
		"SOA",
		"ns1.example.test. admin.example.test. (",
		"2026071501",
		"IN\tNS\tns1.example.test.",
		"203.0.113.10",
		"10 mail.example.test.", // MX priority folded into rdata
		"\"v=DMARC1; p=none\"",  // TXT quoted
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("zone file missing %q:\n%s", want, out)
		}
	}
}

// A TXT value beyond 255 bytes (a DKIM public key is the everyday case) must
// render as multiple quoted character-strings — BIND refuses a single long
// string, and resolvers concatenate the parts (RFC 1035).
func TestRenderZoneSplitsLongTXT(t *testing.T) {
	long := "v=DKIM1; k=rsa; p=" + strings.Repeat("A", 380)
	z := &dns.ZoneRow{
		Name: "example.test", PrimaryNS: "ns1.example.test", AdminEmail: "admin@example.test",
		Serial: 1, Refresh: 3600, Retry: 900, Expire: 1209600, Minimum: 300, TTL: 3600,
	}
	out := dns.RenderZoneFile(z, []dns.RecordRow{{Name: "hp1._domainkey", Type: "TXT", Content: long, TTL: 3600}})

	var txtLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "hp1._domainkey") {
			txtLine = line
		}
	}
	if txtLine == "" {
		t.Fatalf("no TXT line rendered:\n%s", out)
	}
	if strings.Count(txtLine, `"`) < 4 {
		t.Errorf("long TXT was not split into multiple quoted strings: %s", txtLine)
	}
	// Each quoted character-string must be ≤255 bytes (BIND's per-string limit).
	// Odd indices of a split on `"` are the quoted contents.
	segs := strings.Split(txtLine, `"`)
	for i := 1; i < len(segs); i += 2 {
		if len(segs[i]) > 255 {
			t.Errorf("a character-string exceeds 255 bytes: %d", len(segs[i]))
		}
	}
	// The full value survives when the parts are concatenated.
	joined := strings.ReplaceAll(txtLine, `" "`, "")
	if !strings.Contains(joined, strings.Repeat("A", 380)) {
		t.Error("the split TXT lost content")
	}
}

func TestRenderNamedConf(t *testing.T) {
	out := dns.RenderNamedConf([]dns.ZoneRow{{Name: "example.test"}, {Name: "foo.test"}})
	for _, want := range []string{
		`zone "example.test" {`,
		`file "/etc/bind/zones/db.example.test";`,
		`zone "foo.test" {`,
		"type master;",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("named conf missing %q:\n%s", want, out)
		}
	}
}
