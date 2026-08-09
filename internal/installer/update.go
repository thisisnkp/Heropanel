package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Self-update, the privileged half. npd has already downloaded a release into a
// staging directory and verified its signature and checksums; this replaces the
// running binaries with it.
//
// It runs as a transient systemd unit started by the broker (see
// broker/capabilities/panelupdate.go), which is the only way the swap can
// outlive the processes being swapped. Nothing here may assume npd or the
// broker is alive at any point — including to report back, which is why the
// outcome goes to a file the panel reads when it returns.
//
// The contract is: **either the new version is answering, or the old one is.**
// Everything below exists to keep that true.

// UpdateOptions parameterises an update run.
type UpdateOptions struct {
	// Source is the staged, already-verified release directory.
	Source string
	// UID identifies the panel_updates row this run settles.
	UID string
	// Version is the release being installed; informational, used in the result.
	Version string
	// HealthDeadline bounds how long the new panel gets to come up before the
	// update is judged failed. Two service restarts and a migration run make
	// this necessarily longer than an install's verify.
	HealthDeadline time.Duration
	// RollbackDeadline bounds the confirmation that the restored panel came
	// back. It is shorter than HealthDeadline because the old version has
	// already run on this host and has no migrations left to apply — and
	// because the outcome does not depend on it: the previous bytes are on disk
	// either way, this only decides how loudly the log says so.
	RollbackDeadline time.Duration
}

// Default budgets. Both are fields rather than constants so tests can drive the
// gate and the rollback without waiting minutes of real time.
const (
	updateHealthDeadline   = 90 * time.Second
	updateRollbackDeadline = 60 * time.Second
)

// Update swaps in a staged release and proves the result, restoring the previous
// binaries if the new ones do not come up.
func (e *Executor) Update(ctx context.Context, o UpdateOptions) UpdateOutcome {
	if o.HealthDeadline <= 0 {
		o.HealthDeadline = updateHealthDeadline
	}
	if o.RollbackDeadline <= 0 {
		o.RollbackDeadline = updateRollbackDeadline
	}
	prevVersion := e.installedVersion(ctx)
	if o.Source == "" {
		return e.finish(o, UpdateFailed, prevVersion, fmt.Errorf("no staged release directory given"))
	}

	// 1. Re-verify. npd checked this release before staging it, but npd is the
	//    network-facing process — the component that actually overwrites root's
	//    binaries does not take another process's word for what is safe.
	if err := e.verifyStagedRelease(o.Source); err != nil {
		return e.finish(o, UpdateFailed, prevVersion, fmt.Errorf("staged release rejected: %w", err))
	}

	// 2. Snapshot what is running now. This is the rollback, and it is taken
	//    from the live binaries rather than from a previous download, so it is
	//    correct even for an install that was never updated before.
	backupDir := filepath.Join(filepath.Dir(o.Source), "rollback")
	if err := e.snapshotBinaries(backupDir); err != nil {
		return e.finish(o, UpdateFailed, prevVersion, fmt.Errorf("could not snapshot the current binaries: %w", err))
	}
	e.Log.Info("update: current binaries snapshotted", "dir", backupDir)

	// 3. Swap. copyFile writes a temp file and renames, so no binary is ever
	//    half-written — a torn npd would not start at all.
	if err := e.swapBinaries(o.Source); err != nil {
		e.Log.Error("update: swap failed; restoring", "err", err)
		return e.rollback(ctx, o, backupDir, prevVersion, fmt.Errorf("swap failed: %w", err))
	}

	// 4. Restart. The broker first: npd Requires= it, and npd's own restart is
	//    what finally replaces the process running this update's caller.
	if err := e.restartPanel(ctx); err != nil {
		e.Log.Error("update: restart failed; restoring", "err", err)
		return e.rollback(ctx, o, backupDir, prevVersion, fmt.Errorf("restart failed: %w", err))
	}

	// 5. Prove it. Not "did systemctl return 0" — a panel that starts and then
	//    dies on a bad migration would pass that. /readyz answers only when the
	//    datastore and the broker socket are both reachable, and the reported
	//    version proves the process answering is the new one and not a survivor
	//    of a failed restart.
	if err := e.awaitVersion(ctx, o.Version, o.HealthDeadline); err != nil {
		e.Log.Error("update: the new version did not come up; rolling back", "err", err)
		return e.rollback(ctx, o, backupDir, prevVersion, err)
	}

	e.Log.Info("update: complete", "from", prevVersion, "to", o.Version)
	return e.finish(o, UpdateSucceeded, prevVersion, nil)
}

// Update outcome states.
//
// These are declared here rather than imported from internal/update because the
// dependency has to run the other way: internal/update reaches into this package
// to verify a release, so this package cannot reach back. They are the same
// strings, and a test in internal/update asserts that — a wire contract between
// two binaries, pinned rather than assumed.
const (
	UpdateSucceeded  = "succeeded"
	UpdateFailed     = "failed"
	UpdateRolledBack = "rolled_back"
)

// UpdateOutcome is what an update run concluded. The caller (cmd/np-installer)
// records it for the panel to read when it comes back up; this package does not
// write it, because the *format* of that handover belongs to the update domain
// and this package must stay free of it.
type UpdateOutcome struct {
	State       string
	FromVersion string
	ToVersion   string
	Err         error
}

// rollback restores the snapshot and restarts, then reports. A rollback that
// itself fails is the one genuinely bad outcome — the panel is then on neither
// version — so it is recorded as `failed` with the original cause preserved.
func (e *Executor) rollback(ctx context.Context, o UpdateOptions, backupDir, prevVersion string, cause error) UpdateOutcome {
	if err := e.restoreBinaries(backupDir); err != nil {
		return e.finish(o, UpdateFailed, prevVersion,
			fmt.Errorf("%w — and the previous binaries could not be restored: %v", cause, err))
	}
	if err := e.restartPanel(ctx); err != nil {
		return e.finish(o, UpdateFailed, prevVersion,
			fmt.Errorf("%w — and the restored panel could not be restarted: %v", cause, err))
	}
	// Best-effort confirmation that the old version is back. A failure to
	// observe it does not change the outcome — the bytes on disk are the old
	// ones — but it is worth saying so in the record.
	if err := e.awaitReady(ctx, o.RollbackDeadline); err != nil {
		e.Log.Warn("update: rolled back, but the panel did not report ready", "err", err)
	}
	e.Log.Warn("update: rolled back to the previous version", "version", prevVersion)
	return e.finish(o, UpdateRolledBack, prevVersion, cause)
}

// finish builds the outcome. Recording it is the caller's job (see the type's
// doc comment), so this stays a pure assembly of what happened.
func (e *Executor) finish(o UpdateOptions, state, prevVersion string, cause error) UpdateOutcome {
	return UpdateOutcome{State: state, FromVersion: prevVersion, ToVersion: o.Version, Err: cause}
}

// verifyStagedRelease checks the staged directory's signature and checksums with
// exactly the code an install uses.
func (e *Executor) verifyStagedRelease(dir string) error {
	if e.Options.ReleasePubKey == "" {
		// Refusing here is deliberate. At install time an unsigned release is a
		// warning because the operator is standing at the machine having just
		// fetched the artifacts themselves. An update arrives over the network,
		// unattended, and replaces root's binaries — there is no equivalent of
		// that assurance, so an unpinned key means no update.
		return fmt.Errorf("no release public key is pinned, so a downloaded release cannot be trusted")
	}
	pub, err := ParsePublicKey(e.Options.ReleasePubKey)
	if err != nil {
		return fmt.Errorf("release public key: %w", err)
	}
	if err := verifyManifestSignature(dir, pub); err != nil {
		return err
	}
	sums, err := loadChecksumsFrom(dir)
	if err != nil {
		return err
	}
	for _, name := range updateBinaries() {
		if err := VerifyChecksum(filepath.Join(dir, name), sums[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// updateBinaries is what a release replaces. np-installer is included: an
// installer left behind at an old version is the one component that could not
// later fix itself.
func updateBinaries() []string { return []string{"npd", "np-broker", "np-installer"} }

func (e *Executor) snapshotBinaries(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, name := range updateBinaries() {
		src := filepath.Join(e.Layout.BinDir, name)
		if _, err := os.Stat(src); err != nil {
			// A binary that is not installed cannot be rolled back to, but it
			// also cannot have been running — skip rather than fail.
			e.Log.Warn("update: nothing to snapshot", "binary", name)
			continue
		}
		if err := copyFile(src, filepath.Join(dir, name), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) swapBinaries(src string) error {
	for _, name := range updateBinaries() {
		from := filepath.Join(src, name)
		if _, err := os.Stat(from); err != nil {
			return fmt.Errorf("staged release is missing %s", name)
		}
		if err := copyFile(from, filepath.Join(e.Layout.BinDir, name), 0o755); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
	}
	return nil
}

func (e *Executor) restoreBinaries(dir string) error {
	for _, name := range updateBinaries() {
		from := filepath.Join(dir, name)
		if _, err := os.Stat(from); err != nil {
			continue // nothing was snapshotted for this one
		}
		if err := copyFile(from, filepath.Join(e.Layout.BinDir, name), 0o755); err != nil {
			return fmt.Errorf("restore %s: %w", name, err)
		}
	}
	return nil
}

// restartPanel reloads unit files and restarts the broker and then the panel.
func (e *Executor) restartPanel(ctx context.Context) error {
	if e.ServiceManager != "systemd" {
		e.Log.Warn("update: no service manager; binaries swapped but nothing restarted")
		return nil
	}
	if err := e.Runner.Run(ctx, nil, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	for _, unit := range []string{brokerSvc, npdSvc} {
		if err := e.Runner.Run(ctx, nil, "systemctl", "restart", unit); err != nil {
			return fmt.Errorf("restart %s: %w", unit, err)
		}
	}
	return nil
}

// awaitReady polls /readyz until the panel reports healthy. /readyz rather than
// /healthz because the latter answers as soon as the listener is up, which a
// panel with an unreachable database also does.
func (e *Executor) awaitReady(ctx context.Context, budget time.Duration) error {
	if e.ServiceManager != "systemd" && e.probe == nil {
		return nil // nothing was started, so there is nothing to wait for
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/readyz", e.Options.Port)
	deadline := time.Now().Add(budget)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := e.healthProbe(ctx, url); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("the panel did not become ready at %s: %v", url, lastErr)
}

// awaitVersion waits for the panel to be ready *and* to report the version this
// update installed. Readiness alone is not proof: a restart that silently failed
// leaves the old process answering perfectly well.
func (e *Executor) awaitVersion(ctx context.Context, want string, budget time.Duration) error {
	// One budget covers both phases. Splitting it would mean a panel that took
	// most of the window to become ready still got a full second window to
	// report its version, so the caller's deadline would not be the deadline.
	deadline := time.Now().Add(budget)
	if err := e.awaitReady(ctx, budget); err != nil {
		return err
	}
	if want == "" {
		return nil
	}
	var lastErr error
	for time.Now().Before(deadline) {
		got, err := e.reportedVersion(ctx)
		switch {
		case err != nil:
			lastErr = err
		case got == want:
			return nil
		default:
			lastErr = fmt.Errorf("the panel reports version %q, want %q", got, want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return lastErr
}

// versionProbe lets tests report what the "restarted" panel claims to be.
// Unexported and injected the same way probe is.
func (e *Executor) reportedVersion(ctx context.Context) (string, error) {
	if e.versionProbe != nil {
		return e.versionProbe(ctx)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/system/info", e.Options.Port)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("system/info returned %d", resp.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Version, nil
}

// installedVersion reports the version currently on disk, best-effort — it is
// only used to label the record.
func (e *Executor) installedVersion(ctx context.Context) string {
	if v, err := e.reportedVersion(ctx); err == nil && v != "" {
		return v
	}
	return e.Version
}
