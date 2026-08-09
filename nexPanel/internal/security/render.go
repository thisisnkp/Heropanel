package security

import (
	"sort"
	"strconv"
	"strings"
)

// RenderRule is what rendering needs about one firewall rule.
type RenderRule struct {
	Position int
	Action   string // accept | drop
	Protocol string // tcp | udp | any
	Port     int    // 0 = any
	PortEnd  int    // 0 = single port; else the inclusive range end
	Source   string // IPv4/IPv6 address or CIDR; "" = any
}

// RenderRuleset turns the ordered rules into an nftables ruleset for the inet
// "nexpanel" table. The base is **default-drop** on input with the two rules
// no reachable host can live without — established/related and loopback — so a
// ruleset that forgets to allow anything still lets existing connections and
// local traffic through (and the rollback timer catches the rest). forward is
// dropped, output accepted.
//
// Pure and deterministic: rules sort by (position, then their input order), so
// the same rows always render the same bytes and a diff means a real change.
// Comments are deliberately NOT rendered — they are operator metadata, and
// keeping them out of the ruleset removes an escaping/injection surface.
func RenderRuleset(rules []RenderRule, lists []IPListEntry) string {
	ordered := make([]RenderRule, len(rules))
	copy(ordered, rules)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })

	allow4, allow6, block4, block6 := partitionLists(lists)

	var b strings.Builder
	b.WriteString("flush ruleset\n")
	b.WriteString("table inet nexpanel {\n")
	// Named interval sets carry the allow/block CIDRs efficiently — one set
	// membership test covers thousands of ranges (a country block, say).
	writeSet(&b, "np_allow4", "ipv4_addr", allow4)
	writeSet(&b, "np_allow6", "ipv6_addr", allow6)
	writeSet(&b, "np_block4", "ipv4_addr", block4)
	writeSet(&b, "np_block6", "ipv6_addr", block6)
	b.WriteString("\tchain input {\n")
	b.WriteString("\t\ttype filter hook input priority 0; policy drop;\n")
	b.WriteString("\t\tct state established,related accept\n")
	b.WriteString("\t\tiif \"lo\" accept\n")
	// Allow-list wins first (trusted sources are always let in), then block-list
	// drops, then the ordinary rules.
	if len(allow4) > 0 {
		b.WriteString("\t\tip saddr @np_allow4 accept\n")
	}
	if len(allow6) > 0 {
		b.WriteString("\t\tip6 saddr @np_allow6 accept\n")
	}
	if len(block4) > 0 {
		b.WriteString("\t\tip saddr @np_block4 drop\n")
	}
	if len(block6) > 0 {
		b.WriteString("\t\tip6 saddr @np_block6 drop\n")
	}
	for _, r := range ordered {
		b.WriteString("\t\t" + renderRuleLine(r) + "\n")
	}
	b.WriteString("\t}\n")
	b.WriteString("\tchain forward {\n\t\ttype filter hook forward priority 0; policy drop;\n\t}\n")
	b.WriteString("\tchain output {\n\t\ttype filter hook output priority 0; policy accept;\n\t}\n")
	b.WriteString("}\n")
	return b.String()
}

// partitionLists splits entries by mode (allow/block) and family (v4/v6),
// de-duplicating and sorting so the output is deterministic.
func partitionLists(lists []IPListEntry) (allow4, allow6, block4, block6 []string) {
	seen := map[string]bool{}
	for _, e := range lists {
		key := e.Mode + "|" + e.CIDR
		if seen[key] {
			continue
		}
		seen[key] = true
		v6 := isIPv6Source(e.CIDR)
		switch {
		case e.Mode == "allow" && v6:
			allow6 = append(allow6, e.CIDR)
		case e.Mode == "allow":
			allow4 = append(allow4, e.CIDR)
		case v6:
			block6 = append(block6, e.CIDR)
		default:
			block4 = append(block4, e.CIDR)
		}
	}
	sort.Strings(allow4)
	sort.Strings(allow6)
	sort.Strings(block4)
	sort.Strings(block6)
	return
}

// writeSet renders a named interval set holding CIDRs. Nothing is written for an
// empty set, so the ruleset stays minimal.
func writeSet(b *strings.Builder, name, typ string, elems []string) {
	if len(elems) == 0 {
		return
	}
	b.WriteString("\tset " + name + " {\n")
	b.WriteString("\t\ttype " + typ + "; flags interval;\n")
	b.WriteString("\t\telements = { " + strings.Join(elems, ", ") + " }\n")
	b.WriteString("\t}\n")
}

// renderRuleLine renders one match+verdict line. Inputs are already validated.
// The table is `inet` (dual-stack), so a source's address family decides whether
// it matches on `ip saddr` (IPv4) or `ip6 saddr` (IPv6). A port range renders as
// nftables' `dport start-end`.
func renderRuleLine(r RenderRule) string {
	var parts []string
	if r.Source != "" {
		if isIPv6Source(r.Source) {
			parts = append(parts, "ip6 saddr "+r.Source)
		} else {
			parts = append(parts, "ip saddr "+r.Source)
		}
	}
	if r.Protocol == "tcp" || r.Protocol == "udp" {
		switch {
		case r.PortEnd > r.Port && r.Port > 0:
			parts = append(parts, r.Protocol+" dport "+strconv.Itoa(r.Port)+"-"+strconv.Itoa(r.PortEnd))
		case r.Port > 0:
			parts = append(parts, r.Protocol+" dport "+strconv.Itoa(r.Port))
		default:
			parts = append(parts, "meta l4proto "+r.Protocol)
		}
	}
	parts = append(parts, r.Action)
	return strings.Join(parts, " ")
}

// isIPv6Source reports whether a source (address or CIDR) is IPv6.
func isIPv6Source(s string) bool {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return strings.Contains(s, ":")
}
