package capabilities

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Site backups: full and incremental archives via GNU tar.
//
// The privileged half of the backup pipeline is deliberately small: tar a site's
// tree into a staging file (backup.create), and untar an archive into a site's
// tree (backup.restore). Everything clever — encryption, remote upload, chain
// bookkeeping, scheduling — happens in unprivileged hpd, which can be wrong
// without being root. The broker only ever runs tar with arguments it built
// itself from validated inputs.
//
// **Incrementals are GNU tar's own** (--listed-incremental): the broker keeps a
// per-site snapshot file; a full backup resets it (level 0) and an incremental
// diffs against it. Restore replays the chain — full first, then each
// incremental in order — with the documented /dev/null snapshot, which also
// applies deletions recorded in the incrementals. This is battle-tested tar
// behaviour, not a bespoke diff format that would have to earn trust from zero.
//
// Compression is zstd via tar --zstd — the system binary, no Go dependency.

// backupRoot is where staged archives live, owned by the panel user so hpd can
// encrypt/upload them and stage downloads for restore (mirrors dumpRoot). tar
// and install paths are shared with the files/sitedirs capabilities.
const backupRoot = "/var/lib/heropanel/backups"

// reBackupFile is a staged archive name: <ULID>.tar.zst — no directories, no
// dots that climb, so a path built from it cannot leave backupRoot.
var reBackupFile = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}\.tar\.zst$`)

func validateBackupFile(name string) error {
	if !reBackupFile.MatchString(name) {
		return errx.Validation("invalid_backup_file", "Invalid backup filename.")
	}
	return nil
}

// ── backup.create ────────────────────────────────────────────────────────────

// BackupCreate archives a site's tree to a staging file the panel user owns.
type BackupCreate struct{}

func (BackupCreate) Name() string { return "backup.create" }

type backupCreateInput struct {
	Vhost string `json:"vhost"`
	Home  string `json:"home"`
	File  string `json:"file"`  // <ULID>.tar.zst
	Level string `json:"level"` // full | incr
}

func (BackupCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in backupCreateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for backup.create.")
	}
	if err := capability.ValidateVhostName(in.Vhost); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(in.Home, c.Policy); err != nil {
		return capability.Result{}, err
	}
	if err := validateBackupFile(in.File); err != nil {
		return capability.Result{}, err
	}
	if in.Level != "full" && in.Level != "incr" {
		return capability.Result{}, errx.Validation("invalid_level", "Level must be full or incr.")
	}

	panelUser := c.Policy.EffectivePanelUser()
	// Staging dir exists, panel-owned, private.
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: installPath, Args: []string{"-d", "-m", "0700", "-o", panelUser, "-g", panelUser, backupRoot},
		Timeout: 20 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "backup_stage_failed", "Could not prepare the backup directory.")
	}

	snar := backupRoot + "/snap-" + in.Vhost + ".snar"
	if in.Level == "full" {
		// Level 0: forget the old snapshot so tar records everything afresh. A
		// missing snapshot is fine — first backup.
		_ = c.FS.Remove(snar)
	}

	archive := backupRoot + "/" + in.File
	// -C home . archives relative paths, so a restore lands wherever it is
	// pointed rather than at the absolute path of the original site.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: tarPath,
		Args: []string{"--zstd", "--listed-incremental=" + snar, "-cf", archive, "-C", in.Home, "."},
		// A large site takes time; a backup is a long operation by nature.
		Timeout: 60 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "backup_failed", "The backup archive could not be created.")
	}
	// GNU tar exits 1 for "file changed as we read it" — acceptable on a live
	// site; only >=2 is a real failure.
	if res.ExitCode >= 2 {
		return capability.Result{}, errx.New(errx.KindUpstream, "backup_failed",
			"tar failed: "+strings.TrimSpace(string(res.Stderr)))
	}

	// Hand the archive (and the snapshot, which the next incremental needs to
	// survive hpd-side pruning) to the panel user, private.
	for _, args := range [][]string{
		{panelUser + ":" + panelUser, archive},
	} {
		if res, err := c.Runner.Run(c.Ctx, exec.Command{Path: chownPath, Args: args, Timeout: 20 * time.Second}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "backup_stage_failed", "Could not hand the archive to the panel.")
		}
	}
	if res, err := c.Runner.Run(c.Ctx, exec.Command{Path: chmodPath, Args: []string{"0600", archive}, Timeout: 20 * time.Second}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "backup_stage_failed", "Could not restrict the archive.")
	}

	// Size via stat -c %s.
	size := int64(0)
	if res, err := c.Runner.Run(c.Ctx, exec.Command{Path: statPath, Args: []string{"-c", "%s", archive}, Timeout: 20 * time.Second}); err == nil && res.ExitCode == 0 {
		size, _ = strconv.ParseInt(strings.TrimSpace(string(res.Stdout)), 10, 64)
	}
	return capability.Result{Data: map[string]any{"path": archive, "bytes": size, "level": in.Level}}, nil
}

// ── backup.restore ───────────────────────────────────────────────────────────

// BackupRestore extracts one staged archive into a site's tree and re-owns the
// result to the site's user. Restoring an incremental chain is this capability
// called once per archive, oldest first — hpd sequences the chain, the broker
// only ever performs one bounded step.
type BackupRestore struct{}

func (BackupRestore) Name() string { return "backup.restore" }

type backupRestoreInput struct {
	Home     string `json:"home"`
	Username string `json:"username"`
	File     string `json:"file"` // staged archive under backupRoot
}

func (BackupRestore) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in backupRestoreInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for backup.restore.")
	}
	if err := capability.ValidatePath(in.Home, c.Policy); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := validateBackupFile(in.File); err != nil {
		return capability.Result{}, err
	}

	archive := backupRoot + "/" + in.File
	// --listed-incremental=/dev/null is GNU tar's documented restore mode: it
	// applies the archive's incremental metadata (including deletions) without
	// consulting or touching a snapshot.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    tarPath,
		Args:    []string{"--zstd", "--listed-incremental=/dev/null", "-xf", archive, "-C", in.Home},
		Timeout: 60 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "restore_failed", "The archive could not be extracted.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "restore_failed",
			"tar failed: "+strings.TrimSpace(string(res.Stderr)))
	}
	// The extracted files belonged to the ORIGINAL site's uid; they must belong
	// to the site they were restored into.
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: chownPath, Args: []string{"-R", in.Username + ":" + in.Username, in.Home}, Timeout: 10 * time.Minute,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "restore_failed", "Could not re-own the restored files.")
	}
	return capability.Result{Data: map[string]any{"restored": in.File}}, nil
}

// ── backup.prune ─────────────────────────────────────────────────────────────

// BackupPrune deletes one staged archive. hpd owns retention policy; the broker
// only ever deletes a validated filename inside backupRoot.
type BackupPrune struct{}

func (BackupPrune) Name() string { return "backup.prune" }

func (BackupPrune) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for backup.prune.")
	}
	if err := validateBackupFile(in.File); err != nil {
		return capability.Result{}, err
	}
	_ = c.FS.Remove(backupRoot + "/" + in.File) // idempotent
	return capability.Result{Data: map[string]any{"pruned": in.File}}, nil
}
