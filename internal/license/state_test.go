package license

import (
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

func claimsExpiring(at time.Time) *Claims {
	return &Claims{
		LID:   "lic_test",
		Plan:  "pro",
		Feat:  []string{"docker", "mail", "ai"},
		Lim:   Limits{Sites: 50, DBs: 100, Users: 10},
		FP:    "sha256:test",
		IAT:   at.Add(-7 * 24 * time.Hour).Unix(),
		Exp:   at.Unix(),
		Grace: int64(DefaultGrace / time.Second),
		State: "active",
	}
}

// The ladder, at every boundary the product defines. Days are counted from the
// token's expiry, which is what the panel actually reasons from.
func TestLadderBoundaries(t *testing.T) {
	exp := epoch
	snap := Snapshot{Claims: claimsExpiring(exp), Activated: true}

	cases := []struct {
		name  string
		at    time.Time
		state State
	}{
		{"a week before expiry", exp.Add(-7 * 24 * time.Hour), StateActive},
		{"one second before expiry", exp.Add(-time.Second), StateActive},

		// Day 0: expired this instant. Grace begins, and everything still works
		// — this is the rung that makes a failed card a conversation.
		{"day 0", exp, StateGrace},
		{"day 7", exp.Add(7 * 24 * time.Hour), StateGrace},
		{"one second before day 14", exp.Add(14*24*time.Hour - time.Second), StateGrace},

		// Day 14: creating new things stops. Nothing that is running stops.
		{"day 14", exp.Add(14 * 24 * time.Hour), StateDegraded},
		{"day 21", exp.Add(21 * 24 * time.Hour), StateDegraded},
		{"one second before day 30", exp.Add(30*24*time.Hour - time.Second), StateDegraded},

		// Day 30: the panel UI becomes a reactivation page. The websites on the
		// box are still serving; that is asserted separately, in the handlers.
		{"day 30", exp.Add(30 * 24 * time.Hour), StateLocked},
		{"day 31", exp.Add(31 * 24 * time.Hour), StateLocked},
		{"a year later", exp.Add(365 * 24 * time.Hour), StateLocked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(snap, tc.at)
			if got.State != tc.state {
				t.Fatalf("at %s: state = %q, want %q (reason: %s)",
					tc.at.Sub(exp), got.State, tc.state, got.Reason)
			}
		})
	}
}

// A longer grace pushes the whole ladder out rather than eating the degraded
// window. A support agent extending someone's grace means to be kind; under a
// fixed 30-day lock they would have shortened that customer's degraded period.
func TestGraceComesFromTheToken(t *testing.T) {
	exp := epoch
	c := claimsExpiring(exp)
	c.Grace = int64((25 * 24 * time.Hour) / time.Second)
	snap := Snapshot{Claims: c, Activated: true}

	if got := Evaluate(snap, exp.Add(20*24*time.Hour)).State; got != StateGrace {
		t.Fatalf("day 20 of a 25-day grace: got %q, want grace", got)
	}
	if got := Evaluate(snap, exp.Add(26*24*time.Hour)).State; got != StateDegraded {
		t.Fatalf("day 26 of a 25-day grace: got %q, want degraded", got)
	}
	// 25 + 16 = 41, so the lock is a fortnight later than the default ladder,
	// not eleven days earlier.
	if got := Evaluate(snap, exp.Add(40*24*time.Hour)).State; got != StateDegraded {
		t.Fatalf("day 40: got %q, want degraded", got)
	}
	if got := Evaluate(snap, exp.Add(41*24*time.Hour)).State; got != StateLocked {
		t.Fatalf("day 41: got %q, want locked", got)
	}
}

// The whole reason .lstate exists: winding the system clock back must not
// restore a lapsed licence.
func TestClockRollbackCannotRestoreALicence(t *testing.T) {
	exp := epoch
	// The panel has already been running well past the lock boundary.
	lastSeen := exp.Add(45 * 24 * time.Hour)
	snap := Snapshot{Claims: claimsExpiring(exp), Activated: true, LastSeen: lastSeen}

	// Somebody sets the date back to a week before the licence expired.
	got := Evaluate(snap, exp.Add(-7*24*time.Hour))

	if got.State != StateLocked {
		t.Fatalf("state = %q, want locked — the clock was wound back and the licence came alive", got.State)
	}
	if !got.ClockRollback {
		t.Fatal("the rollback was absorbed silently; an operator should be told their clock moved")
	}
}

// A clock nudged backwards while the licence is genuinely current does not
// restore `active`, because a machine whose time is moving backwards is not a
// machine whose time can be trusted to say "not yet expired".
func TestClockRollbackDemotesActive(t *testing.T) {
	exp := epoch.Add(30 * 24 * time.Hour)
	snap := Snapshot{
		Claims:    claimsExpiring(exp),
		Activated: true,
		LastSeen:  epoch.Add(10 * 24 * time.Hour),
	}

	if got := Evaluate(snap, epoch.Add(10*24*time.Hour)).State; got != StateActive {
		t.Fatalf("clock at last_seen: got %q, want active", got)
	}
	// Two days behind the furthest point this install has reached.
	got := Evaluate(snap, epoch.Add(8*24*time.Hour))
	if got.State != StateGrace {
		t.Fatalf("clock two days behind: got %q, want grace", got.State)
	}
	if !got.ClockRollback {
		t.Fatal("ClockRollback was not reported")
	}
}

// Ordinary NTP correction is not an attack.
func TestSmallClockCorrectionIsNotARollback(t *testing.T) {
	exp := epoch.Add(30 * 24 * time.Hour)
	snap := Snapshot{Claims: claimsExpiring(exp), Activated: true, LastSeen: epoch}

	got := Evaluate(snap, epoch.Add(-20*time.Second))
	if got.ClockRollback {
		t.Fatal("a twenty-second correction was reported as a rollback")
	}
	if got.State != StateActive {
		t.Fatalf("state = %q, want active", got.State)
	}
}

// The licence server being unreachable is not a licence problem. Ten days
// offline is well inside a seven-day token plus fourteen days of grace.
func TestOfflineForTenDaysStillWorks(t *testing.T) {
	issued := epoch
	c := claimsExpiring(issued.Add(7 * 24 * time.Hour)) // a normal seven-day lease
	snap := Snapshot{Claims: c, Activated: true, LastSeen: issued}

	for day := 0; day <= 10; day++ {
		at := issued.Add(time.Duration(day) * 24 * time.Hour)
		got := Evaluate(snap, at)
		switch {
		case day < 7 && got.State != StateActive:
			t.Fatalf("day %d offline: state = %q, want active", day, got.State)
		case day >= 7 && got.State != StateGrace:
			t.Fatalf("day %d offline: state = %q, want grace", day, got.State)
		}
		if got.State == StateDegraded || got.State == StateLocked {
			t.Fatalf("day %d offline: the panel degraded while merely offline", day)
		}
	}
}

// Revocation does not wait out the ladder — but it still does not touch a
// single running service.
func TestRevocationLocksImmediately(t *testing.T) {
	exp := epoch.Add(30 * 24 * time.Hour) // a perfectly current licence
	snap := Snapshot{
		Claims:    claimsExpiring(exp),
		Activated: true,
		RevokedAt: epoch,
	}
	got := Evaluate(snap, epoch.Add(time.Minute))
	if got.State != StateLocked {
		t.Fatalf("state = %q, want locked", got.State)
	}
	if !got.Revoked {
		t.Fatal("Revoked was not set")
	}
}

// A token that will not verify gives whoever is probing nothing to iterate
// against: three days of normal behaviour, then the ordinary ladder.
func TestTamperingDegradesSlowlyAndSilently(t *testing.T) {
	start := epoch
	snap := Snapshot{Claims: nil, Activated: true, TamperedSince: start}

	cases := []struct {
		after time.Duration
		state State
	}{
		{0, StateActive},
		{24 * time.Hour, StateActive},
		{TamperGrace - time.Second, StateActive},
		{TamperGrace, StateDegraded},
		{TamperGrace + 10*24*time.Hour, StateDegraded},
		{TamperGrace + DegradedWindow, StateLocked},
	}
	for _, tc := range cases {
		got := Evaluate(snap, start.Add(tc.after))
		if got.State != tc.state {
			t.Fatalf("%s after tampering: state = %q, want %q", tc.after, got.State, tc.state)
		}
		// The entitlements of a token nobody could read must not be handed out.
		if got.Plan != "" || got.Limits != (Limits{}) {
			t.Fatalf("%s after tampering: a plan and limits leaked from an unverifiable token", tc.after)
		}
	}
}

// A machine that has never been activated is locked, and says so in a way that
// sends the operator to the key field rather than to billing.
func TestNeverActivatedIsLockedWithItsOwnMessage(t *testing.T) {
	got := Evaluate(Snapshot{}, epoch)
	if got.State != StateLocked {
		t.Fatalf("state = %q, want locked", got.State)
	}
	if got.Activated {
		t.Fatal("Activated is true on an install that never activated")
	}
	if b := got.Banner(); b == "" || !contains(b, "not been activated") {
		t.Fatalf("banner = %q, want it to say the install is not activated", b)
	}
}

// The banner is the only licence text most operators will ever read, so it has
// to say the thing that matters: your websites are fine.
func TestBannersReassureAboutServices(t *testing.T) {
	exp := epoch
	snap := Snapshot{Claims: claimsExpiring(exp), Activated: true}

	if b := Evaluate(snap, exp.Add(-time.Hour)).Banner(); b != "" {
		t.Fatalf("active state should have no banner, got %q", b)
	}
	degraded := Evaluate(snap, exp.Add(20*24*time.Hour)).Banner()
	if !contains(degraded, "unaffected") {
		t.Fatalf("degraded banner does not reassure about services: %q", degraded)
	}
	locked := Evaluate(snap, exp.Add(40*24*time.Hour)).Banner()
	if !contains(locked, "keep running") {
		t.Fatalf("locked banner does not reassure about services: %q", locked)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
