package capabilities

import "testing"

// grepInt pulls the first integer after a label out of a scanner report — e.g.
// lynis's hardening index from its summary line.
func TestGrepInt(t *testing.T) {
	report := `
  Lynis security scan details:

  Hardening index : 67 [############        ]
  Tests performed : 250
`
	if n, ok := grepInt(report, "Hardening index"); !ok || n != 67 {
		t.Fatalf("hardening index = %d ok=%v, want 67 true", n, ok)
	}
	if n, ok := grepInt(report, "Tests performed"); !ok || n != 250 {
		t.Errorf("tests performed = %d ok=%v, want 250 true", n, ok)
	}
	// A missing label reports not-found rather than a bogus zero.
	if _, ok := grepInt(report, "Nonexistent label"); ok {
		t.Error("a missing label was reported as found")
	}
}
