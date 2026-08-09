package setup

import (
	"strings"
	"testing"
)

const testBase = "panel.example.com"

func TestSuggestTempDomainShape(t *testing.T) {
	got, err := SuggestTempDomain(testBase)
	if err != nil {
		t.Fatalf("SuggestTempDomain: %v", err)
	}
	if !strings.HasSuffix(got, "."+testBase) {
		t.Fatalf("got %q, want a subdomain of %q", got, testBase)
	}
	label := strings.TrimSuffix(got, "."+testBase)
	if !strings.HasPrefix(label, TempDomainPrefix) {
		t.Fatalf("label %q does not start with %q", label, TempDomainPrefix)
	}
	// The suggestion is fed straight into a vhost name, so the label has to be a
	// legal one: lowercase hex after the prefix, nothing else.
	slug := strings.TrimPrefix(label, TempDomainPrefix)
	if want := tempSlugBytes * 2; len(slug) != want {
		t.Fatalf("slug %q is %d chars, want %d", slug, len(slug), want)
	}
	for _, r := range slug {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("slug %q contains a non-hex character %q", slug, r)
		}
	}
}

// A suggestion nobody reserves is only safe if collisions are rare, so this
// guards the entropy rather than the format: a regression to a counter or a
// time-seeded source would show up here immediately.
func TestSuggestTempDomainIsRandom(t *testing.T) {
	const draws = 500
	seen := make(map[string]bool, draws)
	for i := 0; i < draws; i++ {
		got, err := SuggestTempDomain(testBase)
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		if seen[got] {
			t.Fatalf("duplicate suggestion %q within %d draws", got, draws)
		}
		seen[got] = true
	}
}

// An unconfigured panel domain must yield an empty string rather than a
// hostname like "site-a1b2." — callers read empty as "not configured".
func TestSuggestTempDomainWithoutBase(t *testing.T) {
	got, err := SuggestTempDomain("")
	if err != nil {
		t.Fatalf("SuggestTempDomain(\"\"): %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestWildcardFor(t *testing.T) {
	if got := WildcardFor(testBase); got != "*."+testBase {
		t.Fatalf("WildcardFor = %q, want %q", got, "*."+testBase)
	}
	if got := WildcardFor(""); got != "" {
		t.Fatalf("WildcardFor(\"\") = %q, want empty", got)
	}
}

func TestIsTempDomain(t *testing.T) {
	cases := []struct {
		name string
		fqdn string
		base string
		want bool
	}{
		{"minted here", "site-a1b2c3." + testBase, testBase, true},
		{"operator's own subdomain of the base", "blog." + testBase, testBase, false},
		{"different base", "site-a1b2c3.other.example.com", testBase, false},
		{"the base itself", testBase, testBase, false},
		// Guards a suffix-match bug: "notpanel.example.com" ends with
		// "panel.example.com" as a *string* but is a different domain.
		{"suffix collision", "site-a1b2c3.notpanel.example.com", testBase, false},
		{"no base configured", "site-a1b2c3." + testBase, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTempDomain(tc.fqdn, tc.base); got != tc.want {
				t.Fatalf("IsTempDomain(%q, %q) = %v, want %v", tc.fqdn, tc.base, got, tc.want)
			}
		})
	}
}

func TestValidatePanelDomainAndIP(t *testing.T) {
	base := Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB}
	cases := []struct {
		name         string
		domain, ipv4 string
		ok           bool
		wantDomain   string
	}{
		{"both empty is fine", "", "", true, ""},
		{"plain hostname", "panel.example.com", "", true, "panel.example.com"},
		{"normalized", "  Panel.Example.COM.  ", "", true, "panel.example.com"},
		{"with a v4 address", "panel.example.com", "203.0.113.10", true, "panel.example.com"},
		{"wildcard rejected", "*.example.com", "", false, ""},
		{"not a domain", "not a domain", "", false, ""},
		{"bare label", "panel", "", false, ""},
		{"ipv6 rejected — the record is an A", "panel.example.com", "2001:db8::1", false, ""},
		{"garbage ip", "panel.example.com", "999.1.1.1", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sel := base
			sel.PanelDomain, sel.PanelIPv4 = tc.domain, tc.ipv4
			err := sel.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatal("Validate() = nil, want an error")
				}
				return
			}
			if sel.PanelDomain != tc.wantDomain {
				t.Fatalf("PanelDomain = %q, want %q (Validate must normalize in place)", sel.PanelDomain, tc.wantDomain)
			}
		})
	}
}
