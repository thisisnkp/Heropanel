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
