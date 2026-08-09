package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
)

const bkFile = "01HXXXXXXXXXXXXXXXXXXXXXXX.tar.zst"

func backupOK(extra map[string]any) map[string]any {
	in := map[string]any{
		"vhost": "nps1", "home": "/srv/nexpanel/sites/1", "file": bkFile, "level": "full",
	}
	for k, v := range extra {
		in[k] = v
	}
	return in
}

// A full backup resets the per-site snapshot (level 0) and archives with zstd +
// listed-incremental; the staged file is handed to the panel user, private.
func TestBackupCreateFullResetsSnapshotAndStagesPrivately(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	// Pre-existing snapshot from an earlier chain.
	_ = fs.WriteFile("/var/lib/nexpanel/backups/snap-nps1.snar", []byte("old"), 0o600)

	res, err := (capabilities.BackupCreate{}).Execute(appCtx(fr, fs), raw(t, backupOK(nil)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := fs.Written("/var/lib/nexpanel/backups/snap-nps1.snar"); ok {
		t.Error("a FULL backup did not reset the incremental snapshot")
	}

	var tarCall *exec.Command
	for i := range fr.Calls {
		if fr.Calls[i].Path == "/bin/tar" {
			tarCall = &fr.Calls[i]
		}
	}
	if tarCall == nil {
		t.Fatal("tar was never run")
	}
	argv := strings.Join(tarCall.Args, " ")
	for _, want := range []string{
		"--zstd",
		"--listed-incremental=/var/lib/nexpanel/backups/snap-nps1.snar",
		"-cf /var/lib/nexpanel/backups/" + bkFile,
		// Relative paths, so a restore lands wherever it is pointed.
		"-C /srv/nexpanel/sites/1 .",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("tar args missing %q: %v", want, tarCall.Args)
		}
	}
	// chmod 0600 — a backup holds everything the site holds.
	found := false
	for _, call := range fr.Calls {
		if call.Path == "/bin/chmod" && strings.Join(call.Args, " ") == "0600 /var/lib/nexpanel/backups/"+bkFile {
			found = true
		}
	}
	if !found {
		t.Error("the staged archive was not made private (0600)")
	}
	if res.Data["level"] != "full" {
		t.Errorf("level = %v", res.Data["level"])
	}
}

// An incremental keeps the snapshot, so tar diffs against it.
func TestBackupCreateIncrementalKeepsSnapshot(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/var/lib/nexpanel/backups/snap-nps1.snar", []byte("state"), 0o600)
	if _, err := (capabilities.BackupCreate{}).Execute(appCtx(fr, fs), raw(t, backupOK(map[string]any{"level": "incr"}))); err != nil {
		t.Fatalf("incr: %v", err)
	}
	if _, ok := fs.Written("/var/lib/nexpanel/backups/snap-nps1.snar"); !ok {
		t.Error("an incremental backup deleted the snapshot it needs")
	}
}

func TestBackupValidation(t *testing.T) {
	bad := []map[string]any{
		{"file": "../../etc/shadow.tar.zst"},
		{"file": "notaulid.tar.zst"},
		{"file": bkFile + ".sh"},
		{"home": "/etc"},
		{"level": "sideways"},
		{"vhost": "-rf"},
	}
	for _, spoil := range bad {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.BackupCreate{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, backupOK(spoil))); err == nil {
			t.Errorf("accepted bad input %v", spoil)
		}
		for _, call := range fr.Calls {
			if call.Path == "/bin/tar" {
				t.Errorf("ran tar despite bad input %v", spoil)
			}
		}
	}
}

// Restore extracts with the documented /dev/null snapshot (applies deletions)
// and re-owns the tree to the NEW site's user.
func TestBackupRestoreExtractsAndReowns(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.BackupRestore{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"home": "/srv/nexpanel/sites/2", "username": "nps2", "file": bkFile,
	})); err != nil {
		t.Fatalf("restore: %v", err)
	}
	var sawTar, sawChown bool
	for _, call := range fr.Calls {
		argv := strings.Join(call.Args, " ")
		if call.Path == "/bin/tar" {
			sawTar = true
			for _, want := range []string{
				"--listed-incremental=/dev/null",
				"-xf /var/lib/nexpanel/backups/" + bkFile,
				"-C /srv/nexpanel/sites/2",
			} {
				if !strings.Contains(argv, want) {
					t.Errorf("restore tar missing %q: %v", want, call.Args)
				}
			}
		}
		if call.Path == "/bin/chown" && strings.Contains(argv, "-R nps2:nps2 /srv/nexpanel/sites/2") {
			sawChown = true
		}
	}
	if !sawTar || !sawChown {
		t.Errorf("restore ran tar=%v chown=%v; want both", sawTar, sawChown)
	}
}
