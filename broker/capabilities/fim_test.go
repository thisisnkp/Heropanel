package capabilities

import (
	"strings"
	"testing"
)

// The AIDE summary parser pulls the added/removed/changed counts out of the
// report, so the service can say "3 files changed" without re-running the tool.
func TestParseAIDESummary(t *testing.T) {
	report := `AIDE found differences between database and filesystem!!

Summary:
  Total number of entries:	128
  Added entries:		2
  Removed entries:		1
  Changed entries:		3
`
	added, removed, changed := parseAIDESummary(report)
	if added != 2 || removed != 1 || changed != 3 {
		t.Fatalf("parsed added=%d removed=%d changed=%d, want 2/1/3", added, removed, changed)
	}
}

// A clean report parses as all zeros.
func TestParseAIDESummaryClean(t *testing.T) {
	added, removed, changed := parseAIDESummary("AIDE found NO differences between database and filesystem. Looks okay!!\n")
	if added != 0 || removed != 0 || changed != 0 {
		t.Errorf("clean report parsed non-zero: %d/%d/%d", added, removed, changed)
	}
}

// The host scope is a strict superset of the panel scope, and an unknown scope
// falls back to the safe panel default.
func TestRenderFIMConfScope(t *testing.T) {
	panel := renderFIMConf("panel")
	host := renderFIMConf("host")
	if !strings.Contains(panel, "/etc/heropanel HP") {
		t.Fatal("panel scope missing the panel paths")
	}
	if strings.Contains(panel, "/usr/bin HP") {
		t.Fatal("panel scope should not watch the whole binary tree")
	}
	if !strings.Contains(host, "/etc HP") || !strings.Contains(host, "/usr/bin HP") {
		t.Fatal("host scope missing the wider host paths")
	}
	if !strings.Contains(host, "!/var/lib/heropanel") {
		t.Fatal("host scope should exclude the panel's own state dir")
	}
	// An unknown scope is treated as panel.
	if renderFIMConf(normalizeFIMScope("bogus")) != panel {
		t.Fatal("unknown scope did not fall back to panel")
	}
	if normalizeFIMScope("host") != "host" {
		t.Fatal("host scope not preserved")
	}
}

// A very large report is clamped so it cannot bloat the response.
func TestClampReport(t *testing.T) {
	big := make([]byte, 20<<10)
	for i := range big {
		big[i] = 'x'
	}
	out := clampReport(string(big))
	if len(out) >= 20<<10 || !strings.Contains(out, "truncated") {
		t.Errorf("report not clamped: len=%d", len(out))
	}
}
