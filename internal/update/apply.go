package update

import (
	"context"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/semver"
)

// Capability is the one privileged verb self-update needs. It is deliberately
// the narrowest thing that can work: it does not take a command, a unit body or
// a destination — only a staged directory the broker re-validates — so the
// blast radius of the capability is "start the installer", not "run something
// as root".
const Capability = "panel.update"

// Apply hands a staged release to the privileged half and returns as soon as
// the installer has been started.
//
// It returns *before the update finishes*, and that is not a shortcut: the
// installer restarts np-broker and then npd, so the process serving this call
// is about to be replaced. There is no version of this that can report its own
// success. The panel_updates row is the handover — the panel that comes back up
// reads it to learn what was attempted, and the installer writes the outcome
// there rather than to a caller that no longer exists.
func (s *Service) Apply(ctx context.Context, version string) (*Row, error) {
	if err := s.configuredErr(); err != nil {
		return nil, err
	}
	if s.gw == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available, so the panel cannot update itself.")
	}
	if err := validVersion(version); err != nil {
		return nil, err
	}
	if !semver.Newer(s.version, version) {
		return nil, errx.Conflict("not_an_upgrade",
			"That release is not newer than the running version.")
	}
	// One at a time. A second update starting while the first is mid-swap would
	// have two processes writing the same binaries.
	if last, err := s.repo.Latest(ctx); err == nil && last != nil {
		if last.State == StateStaged || last.State == StateApplying {
			return nil, errx.Conflict("update_in_progress",
				"An update to "+last.ToVersion+" is already in progress.")
		}
	}

	dir, err := s.Stage(ctx, version)
	if err != nil {
		return nil, err
	}

	// The database is the part an update cannot undo: binaries roll back by
	// copying the old bytes over, but a migration that has run has run. Take the
	// snapshot before anything is swapped, and refuse to continue if it fails —
	// proceeding would mean an upgrade with no way back.
	if s.snap != nil && s.snap.PanelAvailable() {
		if _, err := s.snap.CreatePanelBackup(ctx); err != nil {
			return nil, errx.Wrap(err, errx.KindInternal, "pre_update_snapshot_failed",
				"Could not take a panel database snapshot before updating, so the update was not started.")
		}
		s.log.Info("self-update: pre-update panel snapshot taken", "version", version)
	}

	row := &Row{
		UID:         newUID(),
		FromVersion: s.version,
		ToVersion:   version,
		Channel:     s.cfg.Channel,
		State:       StateApplying,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		return nil, err
	}

	if _, err := s.gw.Invoke(ctx, Capability, map[string]any{
		"source":  dir,
		"version": version,
		"uid":     row.UID,
	}); err != nil {
		// The installer never started, so nothing was touched — record that
		// plainly rather than leaving a row stuck in "applying" forever.
		_ = s.repo.SetState(ctx, row.UID, StateFailed, reason(err), true)
		row.State, row.Error = StateFailed, reason(err)
		return row, err
	}

	s.log.Warn("self-update: installer started; this process is about to be restarted",
		"from", s.version, "to", version, "uid", row.UID)
	return row, nil
}

// History returns recent update attempts, newest first.
func (s *Service) History(ctx context.Context, limit int) ([]Row, error) {
	if s == nil || s.repo == nil {
		return []Row{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.List(ctx, limit)
}
