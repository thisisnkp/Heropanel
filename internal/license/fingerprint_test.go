package license

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// What this machine is, and — more importantly — what it is *not* allowed to
// be. The corrected rule is that a fingerprint component must survive a VPS
// resize, a live migration and a NIC replacement; anything that does not is a
// soft signal, reported and never scored.

// The regression this file exists for. CPU model, core count, RAM and MAC all
// change when a customer clicks "resize" on their VPS or their provider moves
// the VM to a newer host. Those are things a customer pays to have happen. If
// any of them were a component, that click would change the fingerprint, spend
// an activation slot, and produce a support ticket — so the type system is
// asked to hold the line here rather than a comment.
func TestOnlyPersistentThingsAreComponents(t *testing.T) {
	want := []string{"install_id", "machine_id", "product_uuid", "disk_uuid"}
	if got := jsonNames(Components{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("components = %v, want %v", got, want)
	}

	// The wire contract, spelled out: every one of these belongs on the other
	// side of the line, and a future edit that promotes one of them back into
	// Components fails here.
	soft := jsonNames(SoftSignals{})
	for _, name := range []string{"cpu", "cores", "ram_mb", "mac"} {
		if hasName(jsonNames(Components{}), name) {
			t.Fatalf("%q is scored on; a resize would cost the customer a seat", name)
		}
		if !hasName(soft, name) {
			t.Fatalf("%q is neither a component nor a soft signal — it is not collected at all", name)
		}
	}
}

func TestTheHashChangesWithEveryComponentAndNothingElse(t *testing.T) {
	base := Components{
		InstallID:   "6f1b2c3d-4e5f-4a6b-8c9d-0e1f2a3b4c5d",
		MachineID:   "9a1c0e5d4b3a2f1e0d9c8b7a6f5e4d3c",
		ProductUUID: "3f2b1a09-8c7d-6e5f-4a3b-2c1d0e9f8a7b",
		DiskUUID:    "b41c9e77-2a55-4d13-9f0e-77c1a2b3d4e5",
	}
	original := hashComponents(base)
	if !strings.HasPrefix(original, "sha256:") {
		t.Fatalf("hash = %q, want a sha256: prefix", original)
	}
	if hashComponents(base) != original {
		t.Fatal("the same machine hashed differently twice")
	}

	for name, mutate := range map[string]func(*Components){
		"install_id":   func(c *Components) { c.InstallID = "changed" },
		"machine_id":   func(c *Components) { c.MachineID = "changed" },
		"product_uuid": func(c *Components) { c.ProductUUID = "changed" },
		"disk_uuid":    func(c *Components) { c.DiskUUID = "changed" },
	} {
		c := base
		mutate(&c)
		if hashComponents(c) == original {
			t.Fatalf("changing %s did not change the fingerprint", name)
		}
	}

	// The separator earns its place: without it ("ab","c") and ("a","bc") would
	// hash the same, and two different machines would share a fingerprint.
	a := hashComponents(Components{InstallID: "ab", MachineID: "c"})
	b := hashComponents(Components{InstallID: "a", MachineID: "bc"})
	if a == b {
		t.Fatal("component boundaries collide")
	}
}

func TestPresentCountsWhatCouldBeRead(t *testing.T) {
	if got := (Components{}).Present(); got != 0 {
		t.Fatalf("empty machine present = %d, want 0", got)
	}
	c := Components{InstallID: "a", MachineID: "  ", ProductUUID: "b"}
	if got := c.Present(); got != 2 {
		t.Fatalf("present = %d, want 2 (whitespace is not a reading)", got)
	}
}

// The placeholder filter, which is what makes a 2-of-4 threshold safe. DMI
// tables are full of stock text, and a fleet cut from one template reports the
// same demo UUID on every machine. Sending one is worse than sending nothing:
// the server would count it as agreement between two unrelated servers.
func TestFirmwarePlaceholdersAreNotIdentities(t *testing.T) {
	for _, junk := range []string{
		"", "   ", "0", "None", "unknown", "N/A",
		"To Be Filled By O.E.M.", "Default string", "System Serial Number",
		"uninitialized", "Not Specified",
		"00000000-0000-0000-0000-000000000000",
		"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF",
		"03000200-0400-0500-0006-000700080009",
	} {
		if got := usable(junk); got != "" {
			t.Fatalf("usable(%q) = %q, want an empty reading", junk, got)
		}
	}
	if got := usable("  3f2b1a09-8c7d-6e5f-4a3b-2c1d0e9f8a7b\n"); got != "3f2b1a09-8c7d-6e5f-4a3b-2c1d0e9f8a7b" {
		t.Fatalf("a real UUID was mangled: %q", got)
	}
}

// The install id is the component the whole scheme leans on, because it is the
// only one nothing outside NexPanel can change.
func TestInstallIDIsWrittenOnceAndThenNeverChanges(t *testing.T) {
	dir := t.TempDir()

	first := installID(dir)
	if first == "" {
		t.Fatal("no install id was minted")
	}
	if len(first) != 36 || strings.Count(first, "-") != 4 {
		t.Fatalf("install id = %q, want a UUID", first)
	}

	for i := 0; i < 5; i++ {
		if got := installID(dir); got != first {
			t.Fatalf("call %d returned %q, want the stored %q", i+2, got, first)
		}
	}

	// It is the *file* that is the identity, not this process's memory of it.
	// A daemon restart must not mint a second id for one machine.
	b, err := os.ReadFile(filepath.Join(dir, InstallIDFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != first {
		t.Fatalf("on disk = %q, returned %q", strings.TrimSpace(string(b)), first)
	}
}

func TestTwoMachinesGetTwoInstallIDs(t *testing.T) {
	if a, b := installID(t.TempDir()), installID(t.TempDir()); a == b {
		t.Fatal("two installations were handed the same identity")
	}
}

// Not hypothetical: the daemon starting while an operator runs
// `npd license activate` races here. A plain check-then-write would leave the
// two processes holding different ids for one machine — the sort of bug that
// shows up as a mysteriously spent activation slot and cannot be reproduced.
func TestConcurrentCallersAgreeOnOneInstallID(t *testing.T) {
	dir := t.TempDir()

	const racers = 16
	ids := make([]string, racers)
	var wg sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			ids[i] = installID(dir)
		}(i)
	}
	start.Done()
	wg.Wait()

	for i, got := range ids {
		if got == "" {
			t.Fatalf("racer %d got no id", i)
		}
		if got != ids[0] {
			t.Fatalf("racer %d got %q, racer 0 got %q — one machine, two identities", i, got, ids[0])
		}
	}
}

// Anyone who can read this file can impersonate this machine to the licence
// server; anyone who can write it can make this machine claim to be another.
func TestInstallIDIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("file modes are not meaningful on this platform")
	}
	dir := t.TempDir()
	installID(dir)

	info, err := os.Stat(filepath.Join(dir, InstallIDFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("install id mode = %04o, want 0600", perm)
	}
}

// An unreadable file is "no reading", not "mint a new one". Returning a fresh
// id after failing to store it would give the machine a different identity on
// every boot — and a machine whose identity changes daily burns its
// hardware-change allowance in under a week.
func TestAnUnwritableDirectoryYieldsNoIDRatherThanANewOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "file-not-a-dir")
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := installID(dir); got != "" {
		t.Fatalf("installID = %q, want an empty reading when it cannot be stored", got)
	}
}

// Collect has to survive anything: a container with no /etc/machine-id, a host
// with no DMI tables, a developer's laptop. Refusing to activate because one
// file was unreadable would turn a cosmetic gap into an install nobody can sell
// to.
func TestCollectNeverFailsAndAlwaysProducesAHash(t *testing.T) {
	dir := t.TempDir()
	fp := Collect(dir)

	if !strings.HasPrefix(fp.Hash, "sha256:") || len(fp.Hash) != len("sha256:")+64 {
		t.Fatalf("hash = %q", fp.Hash)
	}
	if fp.Components.InstallID == "" {
		t.Fatal("the install id is the one component we can always mint")
	}
	if fp.Components.Present() < 1 {
		t.Fatal("nothing at all was readable")
	}
	// Same directory, same machine, same fingerprint — the property every
	// heartbeat and every re-activation depends on.
	if again := Collect(dir); again.Hash != fp.Hash {
		t.Fatal("the same installation hashed differently on a second reading")
	}
	// A different install directory is a different installation, even on this
	// one host: the install id is what separates them.
	if other := Collect(t.TempDir()); other.Hash == fp.Hash {
		t.Fatal("two installations share a fingerprint")
	}
}

func TestMACIsNormalisedAndTheAllZeroAddressIsNotAnIdentity(t *testing.T) {
	if got := normaliseMAC("AA:BB:CC:DD:EE:FF"); got != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("normaliseMAC = %q", got)
	}
	for _, empty := range []string{"", "  ", "00:00:00:00:00:00", "00-00-00-00-00-00"} {
		if got := normaliseMAC(empty); got != "" {
			t.Fatalf("normaliseMAC(%q) = %q, want empty", empty, got)
		}
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func jsonNames(v any) []string {
	t := reflect.TypeOf(v)
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		out = append(out, strings.Split(t.Field(i).Tag.Get("json"), ",")[0])
	}
	return out
}

func hasName(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
