package dns

import "testing"

func TestParseZoneFileCommonRecords(t *testing.T) {
	master := `$ORIGIN example.test.
$TTL 3600
@	IN	SOA	ns1.example.test. admin.example.test. ( 2024010101 3600 900 1209600 300 )
@	IN	NS	ns1.example.test.
@	3600	IN	A	203.0.113.10
www	IN	A	203.0.113.20
	IN	AAAA	2001:db8::1
mail	IN	MX	10 mail.example.test.
@	IN	TXT	"v=spf1 -all"
sub.deep	IN	CNAME	www.example.test.
`
	recs, skipped, err := ParseZoneFile("example.test", master)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]ParsedRecord{}
	for _, r := range recs {
		byKey[r.Name+"/"+r.Type] = r
	}
	// SOA and apex NS are the panel's; they must not be imported.
	if _, ok := byKey["@/SOA"]; ok {
		t.Error("SOA must be skipped")
	}
	if _, ok := byKey["@/NS"]; ok {
		t.Error("apex NS must be skipped")
	}
	// Apex A.
	if r, ok := byKey["@/A"]; !ok || r.Content != "203.0.113.10" || r.TTL != 3600 {
		t.Errorf("apex A = %+v", r)
	}
	// Owner inheritance: the AAAA line is indented, so it belongs to www.
	if r, ok := byKey["www/AAAA"]; !ok || r.Content != "2001:db8::1" {
		t.Errorf("inherited-owner AAAA = %+v (want owner www)", r)
	}
	// MX priority split.
	if r, ok := byKey["mail/MX"]; !ok || r.Priority != 10 || r.Content != "mail.example.test." {
		t.Errorf("MX = %+v", r)
	}
	// TXT unquoted.
	if r, ok := byKey["@/TXT"]; !ok || r.Content != "v=spf1 -all" {
		t.Errorf("TXT = %+v (want unquoted)", r)
	}
	if len(skipped) != 0 {
		t.Errorf("unexpected skips: %v", skipped)
	}
}

func TestParseZoneFileFQDNAndOrigin(t *testing.T) {
	// Fully-qualified owner names and a differing $ORIGIN resolve to zone labels.
	master := `$ORIGIN sub.example.test.
host	IN	A	1.2.3.4
other.example.test.	IN	A	5.6.7.8
`
	recs, _, err := ParseZoneFile("example.test", master)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, r := range recs {
		names[r.Name] = r.Content
	}
	// host under $ORIGIN sub.example.test => label "host.sub".
	if names["host.sub"] != "1.2.3.4" {
		t.Errorf("relative-to-origin label wrong: %v", names)
	}
	// FQDN other.example.test => label "other".
	if names["other"] != "5.6.7.8" {
		t.Errorf("FQDN label wrong: %v", names)
	}
}

func TestParseZoneFileSkipsUnsupported(t *testing.T) {
	master := "@ IN A 1.1.1.1\n@ IN DNSKEY 257 3 13 abc\ngarbage line here\n"
	recs, skipped, err := ParseZoneFile("example.test", master)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Errorf("want 1 supported record, got %d", len(recs))
	}
	if len(skipped) != 2 {
		t.Errorf("want 2 skips (DNSKEY + garbage), got %v", skipped)
	}
}

func TestStripCommentKeepsQuotedSemicolon(t *testing.T) {
	if got := stripComment(`@ IN TXT "a;b" ; trailing`); got != `@ IN TXT "a;b" ` {
		t.Errorf("stripComment = %q", got)
	}
}
