package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every external command and returns canned results, so the
// executor's engine logic is tested without touching the real system. failOn
// makes a matching command fail; outputs supplies canned stdout.
type fakeRunner struct {
	calls   []string
	failOn  map[string]bool // key: "name arg0 arg1" prefix match
	outputs map[string]string
}

func newFakeRunner() *fakeRunner {
	// Default to a fresh host: the group/user existence probes fail (absent), so
	// the user step actually creates them. The uid lookup ("id -u heropanel")
	// goes through Output and still returns a value.
	return &fakeRunner{
		failOn:  map[string]bool{"getent group heropanel": true, "id heropanel": true},
		outputs: map[string]string{},
	}
}

func (f *fakeRunner) line(name string, args ...string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

func (f *fakeRunner) Run(_ context.Context, _ []string, name string, args ...string) error {
	l := f.line(name, args...)
	f.calls = append(f.calls, l)
	for k := range f.failOn {
		if strings.HasPrefix(l, k) {
			return &commandError{l}
		}
	}
	return nil
}

func (f *fakeRunner) Output(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
	l := f.line(name, args...)
	f.calls = append(f.calls, l)
	if out, ok := f.outputs[l]; ok {
		return []byte(out), nil
	}
	return []byte("999\n"), nil // default uid
}

func (f *fakeRunner) ran(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

type commandError struct{ cmd string }

func (e *commandError) Error() string { return "fake command failed: " + e.cmd }

// tempExecutor builds an executor whose every path is under a temp dir, a fake
// runner, and staged fake binaries — a fully hermetic install target.
func tempExecutor(t *testing.T, opts Options, r Runner) *Executor {
	t.Helper()
	root := t.TempDir()
	lay := Layout{
		Prefix:     filepath.Join(root, "opt"),
		BinDir:     filepath.Join(root, "opt", "bin"),
		ConfigDir:  filepath.Join(root, "etc"),
		DataDir:    filepath.Join(root, "var", "lib"),
		RunDir:     filepath.Join(root, "run"),
		LogDir:     filepath.Join(root, "var", "log"),
		SystemdDir: filepath.Join(root, "systemd"),
		SourceDir:  filepath.Join(root, "stage"),
		Journal:    filepath.Join(root, "var", "lib", "install-journal.json"),
	}
	// Stage fake binaries so the binaries step has something to copy.
	if err := os.MkdirAll(lay.SourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, b := range installBinaries {
		if err := os.WriteFile(filepath.Join(lay.SourceDir, b), []byte("#!fake\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(lay.SystemdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ex := NewExecutor("test", Profile{OS: "linux", PkgManager: "apt", Arch: "amd64"}, opts, lay)
	ex.Runner = r
	ex.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	ex.ServiceManager = "none" // no systemd in the unit test host
	return ex
}

func TestExecuteFullRunSQLite(t *testing.T) {
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443, NoWebServer: false}, r)

	if err := ex.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Every journal step is done.
	j, err := ex.loadJournal()
	if err != nil {
		t.Fatalf("load journal: %v", err)
	}
	for _, s := range j.Steps {
		if s.Status != StatusDone {
			t.Errorf("step %q status = %s, want done", s.Step.ID, s.Status)
		}
	}

	// Artifacts landed on disk.
	mustExist(t, filepath.Join(ex.Layout.ConfigDir, configFn))
	mustExist(t, filepath.Join(ex.Layout.ConfigDir, secretsFn))
	mustExist(t, filepath.Join(ex.Layout.BinDir, "hpd"))
	mustExist(t, filepath.Join(ex.Layout.BinDir, "hp-broker"))
	mustExist(t, filepath.Join(ex.Layout.SystemdDir, brokerSvc))
	mustExist(t, filepath.Join(ex.Layout.SystemdDir, hpdSvc))

	// The config is a SQLite config pointing at the data dir.
	cfg := readFile(t, filepath.Join(ex.Layout.ConfigDir, configFn))
	if !strings.Contains(cfg, "driver: sqlite") {
		t.Errorf("config missing sqlite driver:\n%s", cfg)
	}
	if !strings.Contains(cfg, "heropanel.db") {
		t.Error("config should reference the sqlite db file")
	}

	// Privileged operations went through the runner.
	for _, want := range []string{"useradd", "chown", "runuser -u heropanel", "apt-get install"} {
		if !r.ran(want) {
			t.Errorf("expected a %q call; calls: %v", want, r.calls)
		}
	}
	// SQLite install must not provision MariaDB.
	if r.ran("mysql") {
		t.Error("sqlite install should not run mysql")
	}
	// No systemd here, so no start attempt.
	if r.ran("systemctl enable") {
		t.Error("should not enable services without a service manager")
	}
}

func TestExecuteMariaDBProvisionsAndStarts(t *testing.T) {
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "mariadb", Port: 8443}, r)
	ex.ServiceManager = "systemd"                                 // pretend systemd is present
	ex.probe = func(context.Context, string) error { return nil } // panel "comes up"

	if err := ex.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !r.ran("mysql -e CREATE DATABASE") {
		t.Errorf("mariadb install should provision the database; calls: %v", r.calls)
	}
	if !r.ran("systemctl daemon-reload") || !r.ran("systemctl enable --now hp-broker.service") || !r.ran("systemctl enable --now hpd.service") {
		t.Errorf("systemd install should reload+enable both units; calls: %v", r.calls)
	}
	cfg := readFile(t, filepath.Join(ex.Layout.ConfigDir, configFn))
	if !strings.Contains(cfg, "driver: mariadb") || !strings.Contains(cfg, "@tcp(127.0.0.1:3306)") {
		t.Errorf("expected a mariadb DSN in config:\n%s", cfg)
	}
}

func TestExecuteResumesSkippingDoneSteps(t *testing.T) {
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443}, r)

	// First run fails at db.migrate.
	r.failOn["runuser -u heropanel"] = true
	if err := ex.Execute(context.Background()); err == nil {
		t.Fatal("expected the first run to fail at db.migrate")
	}
	j, _ := ex.loadJournal()
	var sawFailed bool
	for _, s := range j.Steps {
		if s.Step.ID == "db.migrate" {
			if s.Status != StatusFailed {
				t.Errorf("db.migrate status = %s, want failed", s.Status)
			}
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Fatal("db.migrate step not found")
	}

	// Fix the cause and resume with a fresh runner; the already-done steps must
	// not be re-applied.
	r2 := newFakeRunner()
	ex2 := tempExecutor(t, Options{DB: "sqlite", Port: 9443}, r2)
	ex2.Layout = ex.Layout // same target + journal
	if err := ex2.Execute(context.Background()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	// useradd was done in run 1; resume must skip the user step.
	if r2.ran("useradd") {
		t.Error("resume re-ran the completed user step")
	}
	// db.migrate (the failed step) must be retried on resume.
	if !r2.ran("runuser -u heropanel") {
		t.Error("resume did not retry the failed db.migrate step")
	}
	j2, _ := ex2.loadJournal()
	for _, s := range j2.Steps {
		if s.Status != StatusDone {
			t.Errorf("after resume, step %q = %s, want done", s.Step.ID, s.Status)
		}
	}
}

func TestRollbackReversesInReverseOrder(t *testing.T) {
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "mariadb", Port: 8443}, r)
	ex.ServiceManager = "systemd"
	ex.probe = func(context.Context, string) error { return nil }
	if err := ex.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	r2 := newFakeRunner()
	ex.Runner = r2
	if err := ex.Rollback(context.Background()); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// The reversible effects are undone.
	for _, want := range []string{"systemctl disable --now", "userdel heropanel", "groupdel heropanel", "mysql -e DROP DATABASE"} {
		if !r2.ran(want) {
			t.Errorf("rollback should have run %q; calls: %v", want, r2.calls)
		}
	}
	// Files we created are gone.
	if _, err := os.Stat(filepath.Join(ex.Layout.ConfigDir, configFn)); !os.IsNotExist(err) {
		t.Error("config.yaml should be removed on rollback")
	}
	if _, err := os.Stat(filepath.Join(ex.Layout.SystemdDir, hpdSvc)); !os.IsNotExist(err) {
		t.Error("hpd unit should be removed on rollback")
	}
	if _, err := os.Stat(filepath.Join(ex.Layout.BinDir, "hpd")); !os.IsNotExist(err) {
		t.Error("installed hpd binary should be removed on rollback")
	}

	// Ordering: services reverted before the user is deleted.
	iSvc, iUser := indexOfPrefix(r2.calls, "systemctl disable"), indexOfPrefix(r2.calls, "userdel")
	if iSvc < 0 || iUser < 0 || iSvc > iUser {
		t.Errorf("expected services to be reverted before the user; calls: %v", r2.calls)
	}

	// Journal marks everything reverted.
	j, _ := ex.loadJournal()
	for _, s := range j.Steps {
		if s.Status != StatusReverted {
			t.Errorf("after rollback, step %q = %s, want reverted", s.Step.ID, s.Status)
		}
	}
}

func TestSecretsPersistAcrossProcesses(t *testing.T) {
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "mariadb", Port: 8443}, r)
	if err := ex.applySecrets(context.Background()); err != nil {
		t.Fatalf("applySecrets: %v", err)
	}
	token := ex.secrets["HP_BROKER_TOKEN"]
	if len(token) != 64 {
		t.Fatalf("broker token length = %d, want 64 hex chars", len(token))
	}

	// A fresh executor over the same layout rehydrates the secrets from disk.
	fresh := tempExecutor(t, Options{DB: "mariadb", Port: 8443}, newFakeRunner())
	fresh.Layout = ex.Layout
	fresh.loadSecrets()
	if fresh.secrets["HP_BROKER_TOKEN"] != token {
		t.Error("secrets did not round-trip through secrets.env")
	}
	if fresh.secrets["HP_DB_PASSWORD"] == "" {
		t.Error("mariadb install should persist a db password")
	}
}

func TestBinariesVerifyAgainstManifest(t *testing.T) {
	// A correct SHA256SUMS manifest lets the install proceed.
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443}, r)
	writeManifest(t, ex.Layout.SourceDir, false)
	if err := ex.Execute(context.Background()); err != nil {
		t.Fatalf("execute with a valid manifest: %v", err)
	}
	mustExist(t, filepath.Join(ex.Layout.BinDir, "hpd"))
}

func TestBinariesRejectTamperedManifest(t *testing.T) {
	// A manifest whose hash does not match the staged binary must abort the
	// install at the binaries step — a tampered artifact never lands.
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443}, r)
	writeManifest(t, ex.Layout.SourceDir, true) // tamper hpd's hash
	err := ex.Execute(context.Background())
	if err == nil {
		t.Fatal("expected the install to fail on a checksum mismatch")
	}
	if !strings.Contains(err.Error(), "integrity check failed") {
		t.Fatalf("want an integrity error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(ex.Layout.BinDir, "hpd")); !os.IsNotExist(statErr) {
		t.Error("hpd must not be installed when its checksum does not match")
	}
	j, _ := ex.loadJournal()
	for _, s := range j.Steps {
		if s.Step.ID == "binaries" && s.Status != StatusFailed {
			t.Errorf("binaries step status = %s, want failed", s.Status)
		}
	}
}

// writeManifest writes a SHA256SUMS for the staged binaries; if tamper is set,
// hpd's recorded hash is corrupted.
func writeManifest(t *testing.T, dir string, tamper bool) {
	t.Helper()
	var b strings.Builder
	for _, name := range installBinaries {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		hexsum := hex.EncodeToString(sum[:])
		if tamper && name == "hpd" {
			hexsum = strings.Repeat("0", 64)
		}
		b.WriteString(hexsum + "  " + name + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func indexOfPrefix(calls []string, prefix string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}
