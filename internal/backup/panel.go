package backup

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// Panel self-backup: the panel's own state (its database, the one thing that
// cannot be rebuilt from packages) on the same pipeline as site backups —
// snapshotted, sealed with the same derived key, stored on the same targets,
// swept on the same kind of in-process ticker.
//
// One deliberate asymmetry: restore is NOT an API endpoint. A panel that needs
// its database restored is a panel that cannot be trusted to serve the request
// — recovery is `hpd decrypt` on the sealed object plus the documented manual
// steps (docs/22 §7), with HP_SECRET_KEY as the one thing the operator must
// hold outside the backup itself.

// PanelRecord is the persistence view of one panel snapshot.
type PanelRecord struct {
	UID       string `db:"uid"`
	Target    string `db:"target"`
	RemoteKey string `db:"remote_key"`
	SizeBytes int64  `db:"size_bytes"`
	CreatedAt string `db:"created_at"`
}

// PanelBackup is the API view.
type PanelBackup struct {
	UID       string `json:"uid"`
	Target    string `json:"target"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
}

// PanelRepo persists panel snapshots (oldest first, like site chains).
type PanelRepo interface {
	InsertPanel(ctx context.Context, r *PanelRecord) error
	ListPanel(ctx context.Context) ([]PanelRecord, error)
	DeletePanel(ctx context.Context, uid string) error
}

// PanelSnapshotter writes a plaintext snapshot archive of the panel's own
// state into dir and returns its path. The service seals it and removes the
// plaintext, success or failure. The composition root supplies the dialect-
// appropriate implementation (SQLite file snapshot or a broker mysqldump).
type PanelSnapshotter func(ctx context.Context, dir string) (string, error)

// PanelPolicy drives the panel sweep.
type PanelPolicy struct {
	Target        string
	IntervalHours int
	Keep          int
}

// WithPanel wires panel self-backup onto the service.
func (s *Service) WithPanel(repo PanelRepo, snap PanelSnapshotter, p PanelPolicy) *Service {
	if p.Target == "" {
		p.Target = TargetLocal
	}
	if p.IntervalHours < 1 {
		p.IntervalHours = 24
	}
	if p.Keep < 1 {
		p.Keep = 7
	}
	s.panelRepo, s.panelSnap, s.panelPolicy = repo, snap, p
	return s
}

// PanelAvailable reports whether panel self-backup can run.
func (s *Service) PanelAvailable() bool {
	return s.Available() && s.panelRepo != nil && s.panelSnap != nil
}

// PanelPolicyView returns the active policy for display.
func (s *Service) PanelPolicyView() PanelPolicy { return s.panelPolicy }

func (s *Service) requirePanel() error {
	if s.PanelAvailable() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "panel_backup_unavailable",
		"Panel self-backup needs the broker and a data key (HP_SECRET_KEY).")
}

// panelRemoteKey names a sealed panel snapshot on its target.
func panelRemoteKey(uid string) string { return "panel/" + uid + ".enc" }

// CreatePanelBackup snapshots the panel's state now, seals it, stores it, and
// prunes beyond the retention policy. Every snapshot is full — the panel DB is
// small and a self-contained snapshot is what disaster recovery wants.
func (s *Service) CreatePanelBackup(ctx context.Context) (*PanelBackup, error) {
	if err := s.requirePanel(); err != nil {
		return nil, err
	}
	target, ok := s.targets[s.panelPolicy.Target]
	if !ok {
		return nil, errx.New(errx.KindUnavailable, "target_unavailable",
			"The panel backup target ("+s.panelPolicy.Target+") is not configured.")
	}

	// Best-effort: the staging dir normally exists (the broker creates it on
	// the first site backup), but the panel sweep may run before any site has
	// ever been backed up.
	_ = os.MkdirAll(s.staging, 0o700)

	plainPath, err := s.panelSnap(ctx, s.staging)
	if err != nil {
		return nil, err
	}
	// The plaintext snapshot holds every user row and sealed secret column; it
	// must not outlive this call.
	defer func() { _ = os.Remove(plainPath) }()

	uid := idgen.NewULID()
	sealedPath := filepath.Join(s.staging, uid+".enc")
	size, err := s.sealFile(plainPath, sealedPath)
	if err != nil {
		return nil, errx.Internal(err)
	}
	key := panelRemoteKey(uid)
	if err := s.storeSealed(ctx, target, key, sealedPath, size); err != nil {
		_ = os.Remove(sealedPath)
		return nil, err
	}
	rec := &PanelRecord{UID: uid, Target: s.panelPolicy.Target, RemoteKey: key, SizeBytes: size}
	if err := s.panelRepo.InsertPanel(ctx, rec); err != nil {
		return nil, err
	}
	s.prunePanel(ctx)
	return &PanelBackup{UID: rec.UID, Target: rec.Target, SizeBytes: rec.SizeBytes, CreatedAt: rec.CreatedAt}, nil
}

// ListPanelBackups returns panel snapshots newest first.
func (s *Service) ListPanelBackups(ctx context.Context) ([]PanelBackup, error) {
	if s.panelRepo == nil {
		return []PanelBackup{}, nil
	}
	recs, err := s.panelRepo.ListPanel(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PanelBackup, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		out = append(out, PanelBackup{UID: r.UID, Target: r.Target, SizeBytes: r.SizeBytes, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// DeletePanelBackup removes one snapshot (each stands alone — no chains here).
func (s *Service) DeletePanelBackup(ctx context.Context, uid string) error {
	if err := s.requirePanel(); err != nil {
		return err
	}
	recs, err := s.panelRepo.ListPanel(ctx)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if r.UID == uid {
			if target, ok := s.targets[r.Target]; ok {
				_ = target.Delete(ctx, r.RemoteKey)
			}
			return s.panelRepo.DeletePanel(ctx, uid)
		}
	}
	return errx.NotFound("backup_not_found", "No such panel backup.")
}

// prunePanel best-effort enforces retention, oldest first.
func (s *Service) prunePanel(ctx context.Context) {
	recs, err := s.panelRepo.ListPanel(ctx)
	if err != nil {
		return
	}
	for len(recs) > s.panelPolicy.Keep {
		r := recs[0]
		if target, ok := s.targets[r.Target]; ok {
			_ = target.Delete(ctx, r.RemoteKey)
		}
		if err := s.panelRepo.DeletePanel(ctx, r.UID); err != nil {
			return
		}
		recs = recs[1:]
	}
}

// RunPanelScheduler sweeps hourly: a snapshot whenever the newest is older
// than the policy interval. hpd's own ticker for the same reason the site
// sweep is — the job needs the panel's key.
func (s *Service) RunPanelScheduler(ctx context.Context, log interface{ Info(string, ...any) }) {
	if !s.PanelAvailable() {
		return
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !s.panelDue(ctx) {
				continue
			}
			if _, err := s.CreatePanelBackup(ctx); err == nil && log != nil {
				log.Info("scheduled panel backup completed")
			}
		}
	}
}

// panelDue reports whether the newest snapshot is older than the interval.
func (s *Service) panelDue(ctx context.Context) bool {
	recs, err := s.panelRepo.ListPanel(ctx)
	if err != nil {
		return false
	}
	if len(recs) == 0 {
		return true
	}
	ts, err := time.Parse("2006-01-02 15:04:05", recs[len(recs)-1].CreatedAt)
	if err != nil {
		return true
	}
	return s.now().UTC().Sub(ts) >= time.Duration(s.panelPolicy.IntervalHours)*time.Hour
}
