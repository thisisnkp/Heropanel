package update

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Result is what np-installer leaves behind after an update attempt.
//
// The handover is a file rather than a database write, and deliberately so: the
// installer runs as a detached systemd unit with no panel configuration, no DSN
// and no sealed-secret material. Teaching it to open the panel's database would
// couple the one component that must keep working when the panel is broken to
// the panel's schema — exactly the coupling `npd decrypt` exists to avoid for
// backups. A small file in the data directory both processes already share is
// the honest boundary.
type Result struct {
	UID        string `json:"uid"`
	ToVersion  string `json:"to_version"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
	FinishedAt string `json:"finished_at"`
}

// ResultPath is where the installer writes its outcome and npd reads it.
func ResultPath(dataDir string) string {
	return filepath.Join(dataDir, "updates", "last-result.json")
}

// WriteResult records an update outcome for the panel to pick up. Called by
// np-installer, which is why it takes a directory rather than a Service.
func WriteResult(dataDir string, r Result) error {
	path := ResultPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	// Write-then-rename: npd may read this at any moment, and a half-written
	// file would be parsed as a corrupt result rather than as "not there yet".
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Reconcile settles any update left in flight, and is called once at startup.
//
// It exists because the process that starts an update is destroyed by it: npd
// is restarted mid-swap, so no in-process code path can ever observe its own
// update finishing. Whatever comes back up has to work out what happened from
// evidence, which is what this does.
func (s *Service) Reconcile(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	last, err := s.repo.Latest(ctx)
	if err != nil || last == nil {
		return err
	}
	if last.State != StateApplying && last.State != StateStaged {
		return nil // already settled
	}

	path := ResultPath(s.dataDir)
	b, rerr := os.ReadFile(path)
	if rerr == nil {
		var res Result
		if jerr := json.Unmarshal(b, &res); jerr == nil && res.UID == last.UID {
			if serr := s.repo.SetState(ctx, last.UID, res.State, res.Error, true); serr != nil {
				return serr
			}
			s.log.Info("self-update: reconciled from the installer's result",
				"uid", last.UID, "state", res.State, "to", last.ToVersion)
			_ = os.Remove(path)
			return nil
		}
		// A result for some other attempt, or unparseable: fall through to the
		// version evidence below rather than trusting it.
		_ = os.Remove(path)
	}

	// No usable result — the installer was killed, the box lost power, or the
	// unit never started. The running version is then the only witness, and it
	// is a good one: if this process is the version the update was aiming at,
	// the swap plainly happened.
	state, msg := StateFailed, "The updater did not report an outcome; the panel is still on "+s.version+"."
	if s.version == last.ToVersion {
		state, msg = StateSucceeded, ""
	} else if s.version == last.FromVersion {
		state, msg = StateRolledBack, "The update did not complete; the previous version is running."
	}
	if err := s.repo.SetState(ctx, last.UID, state, msg, true); err != nil {
		return errx.Internal(err)
	}
	s.log.Warn("self-update: settled an unreported attempt from the running version",
		"uid", last.UID, "state", state, "running", s.version, "target", last.ToVersion)
	return nil
}
