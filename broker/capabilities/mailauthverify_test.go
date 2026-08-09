package capabilities

import "testing"

// ensureToken is the fragile part of composing SPF/DMARC into surfaces other
// capabilities own: it must add or remove exactly one token and leave everything
// else — including order — untouched. Proven pure, so it needs no live postfix.
func TestEnsureTokenMilters(t *testing.T) {
	dkim := "inet:localhost:8891"
	both := "inet:localhost:8891,inet:localhost:8893"

	// Add DMARC after the existing DKIM milter (order matters: DMARC reads DKIM).
	if got := ensureToken(dkim, opendmarcMilter, true, tokenAppend); got != both {
		t.Fatalf("add DMARC milter = %q, want %q", got, both)
	}
	// Idempotent: adding when already present is a no-op.
	if got := ensureToken(both, opendmarcMilter, true, tokenAppend); got != both {
		t.Fatalf("re-add DMARC milter = %q, want %q", got, both)
	}
	// Remove leaves just DKIM.
	if got := ensureToken(both, opendmarcMilter, false, tokenAppend); got != dkim {
		t.Fatalf("remove DMARC milter = %q, want %q", got, dkim)
	}
	// Removing when absent is a no-op.
	if got := ensureToken(dkim, opendmarcMilter, false, tokenAppend); got != dkim {
		t.Fatalf("remove-absent DMARC milter = %q, want %q", got, dkim)
	}
	// A value with spaces after commas round-trips trimmed, keeping every entry.
	if got := ensureToken("inet:localhost:8891, inet:localhost:8895", opendmarcMilter, true, tokenAppend); got != "inet:localhost:8891,inet:localhost:8895,inet:localhost:8893" {
		t.Fatalf("comma+space add = %q", got)
	}
}

func TestEnsureTokenRecipientRestrictions(t *testing.T) {
	base := "permit_mynetworks,permit_sasl_authenticated,reject_unauth_destination,permit"
	want := "permit_mynetworks,permit_sasl_authenticated,reject_unauth_destination," + policydSPFPolicy + ",permit"

	// The policy service must land BEFORE the trailing permit, not after it (after
	// permit it would never be reached).
	got := ensureToken(base, policydSPFPolicy, true, tokenBeforePermit)
	if got != want {
		t.Fatalf("insert policy service = %q, want %q", got, want)
	}
	// Idempotent.
	if again := ensureToken(got, policydSPFPolicy, true, tokenBeforePermit); again != want {
		t.Fatalf("re-insert = %q, want %q", again, want)
	}
	// Remove restores the original set exactly.
	if back := ensureToken(got, policydSPFPolicy, false, tokenBeforePermit); back != base {
		t.Fatalf("remove policy service = %q, want %q", back, base)
	}
	// With no trailing permit, it appends at the end.
	noPermit := "permit_mynetworks,reject_unauth_destination"
	if got := ensureToken(noPermit, policydSPFPolicy, true, tokenBeforePermit); got != noPermit+","+policydSPFPolicy {
		t.Fatalf("append without permit = %q", got)
	}
	// An empty starting value yields just the token.
	if got := ensureToken("", policydSPFPolicy, true, tokenBeforePermit); got != policydSPFPolicy {
		t.Fatalf("empty start = %q", got)
	}
}
