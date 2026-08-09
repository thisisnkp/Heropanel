package security

import (
	"strings"
	"testing"
)

// The base ruleset is default-drop but always keeps established/related and
// loopback — a ruleset that forgets to allow anything still lets local traffic
// and existing connections through (and the timer catches the rest).
func TestRenderRulesetBaseIsSafeDefaultDrop(t *testing.T) {
	out := RenderRuleset(nil, nil)
	for _, want := range []string{
		"flush ruleset",
		"table inet nexpanel {",
		"type filter hook input priority 0; policy drop;",
		"ct state established,related accept",
		"iif \"lo\" accept",
		"chain forward {\n\t\ttype filter hook forward priority 0; policy drop;",
		"chain output {\n\t\ttype filter hook output priority 0; policy accept;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("base ruleset missing %q:\n%s", want, out)
		}
	}
}

// Rules render in position order with the right nft match clauses.
func TestRenderRulesetRules(t *testing.T) {
	out := RenderRuleset([]RenderRule{
		{Position: 2, Action: "accept", Protocol: "tcp", Port: 443},
		{Position: 1, Action: "accept", Protocol: "tcp", Port: 22},
		{Position: 3, Action: "drop", Protocol: "any", Source: "10.0.0.0/8"},
		{Position: 4, Action: "accept", Protocol: "udp", Port: 0, Source: "192.168.1.5"},
	}, nil)
	// Order: position 1 (22) must come before position 2 (443).
	i22, i443 := strings.Index(out, "tcp dport 22 accept"), strings.Index(out, "tcp dport 443 accept")
	if i22 < 0 || i443 < 0 || i22 > i443 {
		t.Errorf("rules not in position order:\n%s", out)
	}
	if !strings.Contains(out, "ip saddr 10.0.0.0/8 drop") {
		t.Errorf("source-only drop wrong:\n%s", out)
	}
	// udp with no port becomes an l4proto match, still source-scoped.
	if !strings.Contains(out, "ip saddr 192.168.1.5 meta l4proto udp accept") {
		t.Errorf("udp-any-port rule wrong:\n%s", out)
	}
}

// IPv6 sources render on `ip6 saddr` (the table is inet/dual-stack), and a port
// range renders as nftables' `dport start-end`.
func TestRenderRulesetIPv6AndPortRanges(t *testing.T) {
	out := RenderRuleset([]RenderRule{
		{Position: 1, Action: "accept", Protocol: "tcp", Port: 22, Source: "2001:db8::/32"},
		{Position: 2, Action: "drop", Protocol: "tcp", Port: 8000, PortEnd: 9000, Source: "203.0.113.0/24"},
		{Position: 3, Action: "accept", Protocol: "udp", Port: 5000, PortEnd: 5100},
	}, nil)
	if !strings.Contains(out, "ip6 saddr 2001:db8::/32 tcp dport 22 accept") {
		t.Errorf("IPv6 source not rendered on ip6 saddr:\n%s", out)
	}
	if !strings.Contains(out, "ip saddr 203.0.113.0/24 tcp dport 8000-9000 drop") {
		t.Errorf("port range not rendered:\n%s", out)
	}
	if !strings.Contains(out, "udp dport 5000-5100 accept") {
		t.Errorf("udp port range not rendered:\n%s", out)
	}
}

// Allow/block lists render as nftables interval sets with membership tests, and
// allow is evaluated before block.
func TestRenderRulesetIPLists(t *testing.T) {
	out := RenderRuleset(nil, []IPListEntry{
		{CIDR: "203.0.113.0/24", Mode: "block"},
		{CIDR: "2001:db8:bad::/48", Mode: "block"},
		{CIDR: "10.0.0.0/8", Mode: "allow"},
	})
	for _, want := range []string{
		"set np_block4 {",
		"203.0.113.0/24",
		"set np_block6 {",
		"2001:db8:bad::/48",
		"set np_allow4 {",
		"ip saddr @np_allow4 accept",
		"ip saddr @np_block4 drop",
		"ip6 saddr @np_block6 drop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ruleset missing %q\n%s", want, out)
		}
	}
	// Allow must be evaluated before block.
	if strings.Index(out, "@np_allow4 accept") > strings.Index(out, "@np_block4 drop") {
		t.Error("allow set is evaluated after block")
	}
	// Empty lists emit no set at all.
	if strings.Contains(RenderRuleset(nil, nil), "set np_") {
		t.Error("an empty list still emitted a set")
	}
}
