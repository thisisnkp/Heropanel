package installer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests prove the one contract self-update makes: **either the new
// version is answering, or the old one is.**
//
// They run without systemd, and that is the point — Executor takes an injected
// Runner and two injected probes precisely so the swap, the health gate and the
// rollback can be exercised as logic. What is deliberately *not* covered here is
// the real `systemctl restart` and the real transient unit, which need a live
// init; those are asserted at the argv level instead (see recordingRunner).

// recordingRunner stands in for systemctl and records the argv it was given, so
// a test can assert the exact orchestration without a service manager.
type recordingRunner struct {
	calls  []string
	failOn string // a command whose argv contains this substring fails
}

func (r *recordingRunner) Run(_ context.Context, _ []string, name string, args ...string) error {
	line := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, line)
	if r.failOn != "" && strings.Contains(line, r.failOn) {
		return fmt.Errorf("simulated failure: %s", line)
	}
	return nil
}

func (r *recordingRunner) Output(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return nil, nil
}

func (r *recordingRunner) ran(sub string) bool {
	for _, c := range r.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

// updateFixture builds a temp install: a BinDir holding "old" binaries and a
// staged, correctly signed release holding "new" ones.
type updateFixture struct {
	ex      *Executor
	runner  *recordingRunner
	binDir  string
	stage   string
	oldByte string
	newByte string
}

func newUpdateFixture(t *testing.T, version string) *updateFixture {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	dataDir := filepath.Join(root, "data")
	stage := filepath.Join(dataDir, "updates", version)
	for _, d := range []string{binDir, stage} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	const oldByte, newByte = "OLD-BINARY", "NEW-BINARY"
	for _, name := range updateBinaries() {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(oldByte+" "+name), 0o755); err != nil {
			t.Fatalf("write old %s: %v", name, err)
		}
	}

	// A staged release, signed the way a real one is.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	var lines []string
	for _, name := range updateBinaries() {
		body := []byte(newByte + " " + name)
		if err := os.WriteFile(filepath.Join(stage, name), body, 0o755); err != nil {
			t.Fatalf("write staged %s: %v", name, err)
		}
		sum := sha256.Sum256(body)
		lines = append(lines, fmt.Sprintf("%s  %s", hex.EncodeToString(sum[:]), name))
	}
	sums := []byte(strings.Join(lines, "\n") + "\n")
	writeStaged(t, stage, "SHA256SUMS", sums)
	writeStaged(t, stage, "SHA256SUMS.sig",
		[]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums))))

	runner := &recordingRunner{}
	ex := &Executor{
		Version: "1.0.0",
		Options: Options{Port: 8443, ReleasePubKey: base64.StdEncoding.EncodeToString(pub)},
		Layout:  Layout{BinDir: binDir, DataDir: dataDir, SourceDir: stage},
		Runner:  runner,
		// A logger that discards: these tests exercise failure paths on purpose
		// and their warnings are not signal.
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceManager: "systemd",
	}
	return &updateFixture{ex: ex, runner: runner, binDir: binDir, stage: stage, oldByte: oldByte, newByte: newByte}
}

func writeStaged(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// assertBinaries checks every installed binary starts with the given marker.
func (f *updateFixture) assertBinaries(t *testing.T, marker string) {
	t.Helper()
	for _, name := range updateBinaries() {
		b, err := os.ReadFile(filepath.Join(f.binDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.HasPrefix(string(b), marker) {
			t.Errorf("%s = %q, want it to start with %q", name, b, marker)
		}
	}
}

// The happy path: verified, swapped, restarted, and the new version answers.
func TestUpdateSwapsAndVerifies(t *testing.T) {
	f := newUpdateFixture(t, "1.2.3")
	f.ex.probe = func(context.Context, string) error { return nil }
	f.ex.versionProbe = func(context.Context) (string, error) { return "1.2.3", nil }

	out := f.ex.Update(context.Background(), UpdateOptions{Source: f.stage, UID: "u1", Version: "1.2.3"})
	if out.Err != nil {
		t.Fatalf("Update: %v", out.Err)
	}
	if out.State != UpdateSucceeded {
		t.Errorf("state = %q, want %q", out.State, UpdateSucceeded)
	}
	f.assertBinaries(t, f.newByte)

	// The broker must be restarted before npd — npd Requires= it.
	if !f.runner.ran("daemon-reload") {
		t.Error("expected a daemon-reload before restarting")
	}
	bi, hi := indexOf(f.runner.calls, brokerSvc), indexOf(f.runner.calls, npdSvc)
	if bi < 0 || hi < 0 {
		t.Fatalf("expected both services restarted, got %v", f.runner.calls)
	}
	if bi > hi {
		t.Errorf("np-broker must restart before npd, got %v", f.runner.calls)
	}
}

// The core rollback contract: the new panel never becomes ready, so the
// previous bytes must be back on disk.
func TestUpdateRollsBackWhenPanelNeverBecomesReady(t *testing.T) {
	f := newUpdateFixture(t, "1.2.3")
	failing := errors.New("connection refused")
	// Ready never succeeds. The budget is tiny so the test does not wait 90s.
	f.ex.probe = func(context.Context, string) error { return failing }
	f.ex.versionProbe = func(context.Context) (string, error) { return "", failing }

	out := f.ex.Update(context.Background(), UpdateOptions{
		Source: f.stage, UID: "u1", Version: "1.2.3",
		HealthDeadline: 50 * time.Millisecond, RollbackDeadline: 50 * time.Millisecond,
	})
	if out.Err == nil {
		t.Fatal("Update reported success even though the panel never came up")
	}
	if out.State != UpdateRolledBack {
		t.Errorf("state = %q, want %q", out.State, UpdateRolledBack)
	}
	// This is the assertion the whole feature rests on.
	f.assertBinaries(t, f.oldByte)
}

// A subtler failure: the panel is perfectly healthy, but it is the *old*
// process — the restart silently did not take. Readiness alone would pass here,
// which is exactly why the gate also checks the reported version.
func TestUpdateRollsBackWhenOldVersionStillAnswers(t *testing.T) {
	f := newUpdateFixture(t, "1.2.3")
	f.ex.probe = func(context.Context, string) error { return nil }
	f.ex.versionProbe = func(context.Context) (string, error) { return "1.0.0", nil }

	out := f.ex.Update(context.Background(), UpdateOptions{
		Source: f.stage, UID: "u1", Version: "1.2.3",
		HealthDeadline: 50 * time.Millisecond, RollbackDeadline: 50 * time.Millisecond,
	})
	if out.Err == nil {
		t.Fatal("Update accepted a panel still reporting the old version")
	}
	if out.State != UpdateRolledBack {
		t.Errorf("state = %q, want %q", out.State, UpdateRolledBack)
	}
	f.assertBinaries(t, f.oldByte)
}

// A restart that fails outright must also restore.
func TestUpdateRollsBackWhenRestartFails(t *testing.T) {
	f := newUpdateFixture(t, "1.2.3")
	f.runner.failOn = "restart " + npdSvc
	f.ex.probe = func(context.Context, string) error { return nil }
	f.ex.versionProbe = func(context.Context) (string, error) { return "1.2.3", nil }

	out := f.ex.Update(context.Background(), UpdateOptions{Source: f.stage, UID: "u1", Version: "1.2.3"})
	if out.Err == nil {
		t.Fatal("Update reported success even though a restart failed")
	}
	f.assertBinaries(t, f.oldByte)
}

// An unsigned or wrongly-signed staged release must be refused *before*
// anything is touched — the binaries on disk stay exactly as they were.
func TestUpdateRefusesUnverifiedRelease(t *testing.T) {
	f := newUpdateFixture(t, "1.2.3")
	// Tamper with a staged binary after it was signed.
	if err := os.WriteFile(filepath.Join(f.stage, "np-broker"), []byte("BACKDOOR"), 0o755); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	out := f.ex.Update(context.Background(), UpdateOptions{Source: f.stage, UID: "u1", Version: "1.2.3"})
	if out.Err == nil {
		t.Fatal("Update accepted a tampered staged release")
	}
	if out.State != UpdateFailed {
		t.Errorf("state = %q, want %q", out.State, UpdateFailed)
	}
	f.assertBinaries(t, f.oldByte)
	if len(f.runner.calls) != 0 {
		t.Errorf("nothing should have been restarted, got %v", f.runner.calls)
	}
}

// An update with no pinned key must be refused. At install time an unsigned
// release is only a warning; arriving over the network unattended it is not.
func TestUpdateRefusesWithNoPinnedKey(t *testing.T) {
	f := newUpdateFixture(t, "1.2.3")
	f.ex.Options.ReleasePubKey = ""

	out := f.ex.Update(context.Background(), UpdateOptions{Source: f.stage, UID: "u1", Version: "1.2.3"})
	if out.Err == nil {
		t.Fatal("Update proceeded with no release key pinned")
	}
	if !strings.Contains(out.Err.Error(), "public key") {
		t.Errorf("error should name the missing key, got %v", out.Err)
	}
	f.assertBinaries(t, f.oldByte)
}

func indexOf(calls []string, sub string) int {
	for i, c := range calls {
		if strings.Contains(c, sub) {
			return i
		}
	}
	return -1
}
