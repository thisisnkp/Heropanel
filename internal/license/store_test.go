package license

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, Keyring, func(Claims) string) {
	t.Helper()
	ring, priv := testKeyring(t)
	st, err := NewStore(t.TempDir(), ring)
	if err != nil {
		t.Fatal(err)
	}
	return st, ring, func(c Claims) string { return mint(t, priv, "lk1", c) }
}

func TestFreshInstallIsUnactivated(t *testing.T) {
	st, _, _ := newTestStore(t)

	snap := st.Snapshot(epoch)
	if snap.Activated {
		t.Fatal("a directory with no files reported an activated install")
	}
	if snap.Claims != nil {
		t.Fatal("claims appeared from nowhere")
	}
	if Evaluate(snap, epoch).State != StateLocked {
		t.Fatal("a fresh install should be locked until a key is entered")
	}
}

func TestActivationPersistsAndVerifies(t *testing.T) {
	st, _, sign := newTestStore(t)
	claims := validClaims(epoch)
	token := sign(claims)

	if err := st.SaveActivation(token, claims.LID, claims.FP, "nxi_secret", claims, epoch); err != nil {
		t.Fatal(err)
	}

	snap := st.Snapshot(epoch.Add(time.Hour))
	if !snap.Activated || snap.Claims == nil {
		t.Fatalf("activation did not survive: %+v", snap)
	}
	if snap.Claims.LID != claims.LID {
		t.Fatalf("lid = %q", snap.Claims.LID)
	}

	lid, fp, secret, err := st.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if lid != claims.LID || fp != claims.FP || secret != "nxi_secret" {
		t.Fatalf("identity = %q %q %q", lid, fp, secret)
	}
}

// A file mode is not a detail here: the state file carries the heartbeat
// credential, and a world-readable one hands it to every shell user on a box
// whose whole purpose is hosting other people's code.
func TestFilesAreNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	dir := t.TempDir()
	ring, priv := testKeyring(t)
	st, err := NewStore(dir, ring)
	if err != nil {
		t.Fatal(err)
	}
	claims := validClaims(epoch)
	if err := st.SaveActivation(mint(t, priv, "lk1", claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{TokenFile, StateFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s has mode %o, want 600", name, perm)
		}
	}
}

// Corruption is the ordinary case — a power cut mid-write, a botched backup
// restore — and it must not throw an error at the operator or brick the panel.
// It behaves normally for three days, which is plenty of time for a heartbeat
// to replace the file.
func TestCorruptTokenFileDegradesQuietly(t *testing.T) {
	dir := t.TempDir()
	ring, priv := testKeyring(t)
	st, err := NewStore(dir, ring)
	if err != nil {
		t.Fatal(err)
	}
	claims := validClaims(epoch)
	if err := st.SaveActivation(mint(t, priv, "lk1", claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}

	for _, corruption := range []string{
		"",                      // truncated to nothing by a full disk
		"not a token at all",    // a config file copied over it
		"a.b.c",                 // the right shape, wrong content
		string([]byte{0, 1, 2}), // binary noise
	} {
		if err := os.WriteFile(filepath.Join(dir, TokenFile), []byte(corruption), 0o600); err != nil {
			t.Fatal(err)
		}
		// Clear the tamper mark so each corruption is judged from its own start.
		clearTamperMark(t, dir)

		snap := st.Snapshot(epoch.Add(time.Hour))
		if snap.Claims != nil {
			t.Fatalf("%q was accepted as a token", corruption)
		}
		if !snap.Activated {
			t.Fatalf("%q made the install look like it was never activated", corruption)
		}

		// Nothing visible happens for three days.
		if got := Evaluate(snap, epoch.Add(time.Hour)).State; got != StateActive {
			t.Fatalf("%q: state = %q immediately, want active — that is feedback to a prober", corruption, got)
		}
		if got := Evaluate(snap, epoch.Add(TamperGrace+2*time.Hour)).State; got != StateDegraded {
			t.Fatalf("%q: state = %q after three days, want degraded", corruption, got)
		}
	}
}

// A deleted token is tampering on an activated machine, and simply "no licence
// yet" on one that never activated. Same missing file, opposite meanings.
func TestDeletedTokenOnAnActivatedInstall(t *testing.T) {
	dir := t.TempDir()
	ring, priv := testKeyring(t)
	st, _ := NewStore(dir, ring)
	claims := validClaims(epoch)
	if err := st.SaveActivation(mint(t, priv, "lk1", claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, TokenFile)); err != nil {
		t.Fatal(err)
	}

	snap := st.Snapshot(epoch.Add(time.Hour))
	if !snap.Activated || snap.Claims != nil {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.TamperedSince.IsZero() {
		t.Fatal("the deletion was not recorded")
	}
	if got := Evaluate(snap, epoch.Add(time.Hour)).State; got != StateActive {
		t.Fatalf("state = %q, want active for the first three days", got)
	}
}

// An old lease copied back over a newer one — usually alongside a clock change
// — is the cheapest way to try to unwind an expiry. Refused on read.
func TestAReplayedOlderTokenIsRefused(t *testing.T) {
	dir := t.TempDir()
	ring, priv := testKeyring(t)
	st, _ := NewStore(dir, ring)

	old := validClaims(epoch)
	current := validClaims(epoch.Add(30 * 24 * time.Hour))

	if err := st.SaveActivation(mint(t, priv, "lk1", current), current.LID, current.FP, "nxi_s", current, current.IssuedAt()); err != nil {
		t.Fatal(err)
	}
	// The attacker restores last month's token.
	if err := os.WriteFile(filepath.Join(dir, TokenFile), []byte(mint(t, priv, "lk1", old)), 0o600); err != nil {
		t.Fatal(err)
	}

	snap := st.Snapshot(current.IssuedAt().Add(time.Hour))
	if snap.Claims != nil {
		t.Fatal("a token issued before this install's last known time was accepted")
	}
	if snap.TamperedSince.IsZero() {
		t.Fatal("the replay was not recorded as tampering")
	}
}

// SaveToken applies the same rule on write, so a replayed *response* never
// reaches the disk in the first place.
func TestSaveTokenRefusesAStaleLease(t *testing.T) {
	st, _, sign := newTestStore(t)
	current := validClaims(epoch.Add(30 * 24 * time.Hour))
	if err := st.SaveActivation(sign(current), current.LID, current.FP, "nxi_s", current, current.IssuedAt()); err != nil {
		t.Fatal(err)
	}

	old := validClaims(epoch)
	if err := st.SaveToken(sign(old), old, current.IssuedAt()); err == nil {
		t.Fatal("a stale token was written to disk")
	}
}

// The monotonic floor only ever moves forward, and it is what the ladder is
// measured against when the wall clock disagrees.
func TestLastSeenNeverGoesBackwards(t *testing.T) {
	st, _, sign := newTestStore(t)
	claims := validClaims(epoch)
	if err := st.SaveActivation(sign(claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}

	// A month passes with the panel running.
	future := epoch.Add(30 * 24 * time.Hour)
	st.Touch(future)

	// The clock is wound back a year.
	rolledBack := epoch.Add(-365 * 24 * time.Hour)
	snap := st.Snapshot(rolledBack)
	if snap.LastSeen.Before(future) {
		t.Fatalf("last_seen = %s, want no earlier than %s", snap.LastSeen, future)
	}

	// The state is whatever it was at the furthest point reached — the clock
	// change buys nothing at all, which is stronger than "it is still locked":
	// it says the wound-back clock is not an input to the answer.
	rolled := Evaluate(snap, rolledBack)
	honest := Evaluate(snap, future)
	if rolled.State != honest.State {
		t.Fatalf("state = %q with the clock wound back, %q at the real time — the rollback changed the answer",
			rolled.State, honest.State)
	}
	if rolled.State == StateActive {
		t.Fatal("winding the clock back to before the licence was issued restored it")
	}
	if !rolled.ClockRollback {
		t.Fatal("the rollback was absorbed silently; an operator should be told their clock moved")
	}
}

// Deactivating gives the slot back but does not reset the clock. Otherwise
// deactivate-then-reactivate would be the cheapest way to unwind it.
func TestClearKeepsTheMonotonicFloor(t *testing.T) {
	st, _, sign := newTestStore(t)
	claims := validClaims(epoch)
	if err := st.SaveActivation(sign(claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}
	future := epoch.Add(60 * 24 * time.Hour)
	st.Touch(future)

	if err := st.Clear(); err != nil {
		t.Fatal(err)
	}

	snap := st.Snapshot(epoch)
	if snap.Activated {
		t.Fatal("the install still looks activated after Clear")
	}
	if _, _, _, err := st.Identity(); err == nil {
		t.Fatal("the installation secret survived Clear")
	}
	if snap.LastSeen.Before(future) {
		t.Fatalf("Clear reset the monotonic floor to %s", snap.LastSeen)
	}
}

// Revocation has to survive a restart, or `systemctl restart npd` would be the
// documented workaround for it.
func TestRevocationPersists(t *testing.T) {
	dir := t.TempDir()
	ring, priv := testKeyring(t)
	st, _ := NewStore(dir, ring)
	claims := validClaims(epoch.Add(30 * 24 * time.Hour))
	if err := st.SaveActivation(mint(t, priv, "lk1", claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkRevoked(epoch); err != nil {
		t.Fatal(err)
	}

	// A fresh Store over the same directory is what a restart looks like.
	restarted, _ := NewStore(dir, ring)
	snap := restarted.Snapshot(epoch.Add(time.Minute))
	if snap.RevokedAt.IsZero() {
		t.Fatal("revocation did not survive a restart")
	}
	if Evaluate(snap, epoch.Add(time.Minute)).State != StateLocked {
		t.Fatal("a revoked licence is not locked after a restart")
	}
}

// A state file mangled by hand costs the monotonic floor but must not stop the
// panel booting. The alternative — refusing to start over a stray byte in a
// JSON file — is an outage.
func TestCorruptStateFileIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	ring, priv := testKeyring(t)
	st, _ := NewStore(dir, ring)
	claims := validClaims(epoch)
	if err := st.SaveActivation(mint(t, priv, "lk1", claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, StateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(dir, ring)
	if err != nil {
		t.Fatalf("NewStore refused to open over a corrupt state file: %v", err)
	}
	snap := reopened.Snapshot(epoch.Add(time.Hour))
	if snap.Claims == nil {
		t.Fatal("a corrupt state file took the valid token with it")
	}
}

// clearTamperMark resets only the tamper timestamp, leaving everything else, so
// a test can judge several corruptions from the same starting point.
func clearTamperMark(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, StateFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	p.TamperedSince = 0
	out, _ := json.Marshal(&p)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A valid lease must survive being read again, and again, for as long as it is
// the lease.
//
// This is here because it did not. The replay rule originally compared the
// token's `iat` against `last_seen` — the monotonic clock — which advances on
// every read. A perfectly good lease therefore condemned itself as a replay one
// second after it was written, and the panel started its silent slide toward
// degraded while the licence server was answering happily. Freshness is a
// property of the lease, so it is compared against the lease it replaced.
func TestAValidLeaseSurvivesRepeatedReads(t *testing.T) {
	st, _, sign := newTestStore(t)
	claims := validClaims(epoch)
	if err := st.SaveActivation(sign(claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}

	// Six days of the panel running, reading its own lease each hour.
	for h := 1; h <= 6*24; h++ {
		at := epoch.Add(time.Duration(h) * time.Hour)
		snap := st.Snapshot(at)
		if snap.Claims == nil {
			t.Fatalf("hour %d: the lease stopped verifying against itself", h)
		}
		if !snap.TamperedSince.IsZero() {
			t.Fatalf("hour %d: a valid lease was recorded as tampering", h)
		}
		if got := Evaluate(snap, at).State; got != StateActive {
			t.Fatalf("hour %d: state = %q, want active", h, got)
		}
	}
}

// A newer lease replaces an older one, and the older one cannot come back.
func TestANewerLeaseReplacesTheOlderAndClosesTheDoor(t *testing.T) {
	dir := t.TempDir()
	ring, priv := testKeyring(t)
	st, _ := NewStore(dir, ring)

	first := validClaims(epoch)
	if err := st.SaveActivation(mint(t, priv, "lk1", first), first.LID, first.FP, "nxi_s", first, epoch); err != nil {
		t.Fatal(err)
	}

	// A heartbeat six days later brings a fresh lease.
	later := epoch.Add(6 * 24 * time.Hour)
	second := validClaims(later)
	if err := st.SaveToken(mint(t, priv, "lk1", second), second, later); err != nil {
		t.Fatal(err)
	}
	if snap := st.Snapshot(later); snap.Claims == nil || snap.Claims.IAT != second.IAT {
		t.Fatalf("the fresh lease was not accepted: %+v", snap.Claims)
	}

	// Putting the first one back is a replay.
	if err := os.WriteFile(filepath.Join(dir, TokenFile), []byte(mint(t, priv, "lk1", first)), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := st.Snapshot(later.Add(time.Hour))
	if snap.Claims != nil {
		t.Fatal("the superseded lease was accepted again")
	}
	if snap.TamperedSince.IsZero() {
		t.Fatal("the replay was not recorded")
	}
}
