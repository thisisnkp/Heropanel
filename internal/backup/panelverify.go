package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/thisisnkp/heropanel/pkg/blobcrypt"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// A backup you have never restored is not a backup — it is a hope. These
// helpers prove a sealed panel snapshot is actually recoverable: fetch it back
// from its target, decrypt it with the live key, and confirm it unpacks to a
// well-formed archive with the expected manifest. Every freshly created panel
// backup is verified before it is recorded (see CreatePanelBackup), and the
// latest can be re-checked on demand or at startup, so a broken key, a corrupt
// object, or a silently-empty snapshot is caught now instead of during a real
// disaster.

// panelManifestKind is the marker written into a panel snapshot's manifest.json.
const panelManifestKind = "heropanel-panel-backup"

// VerifyPanelBackup checks that a specific panel snapshot can be decrypted and
// unpacked. It is read-only: nothing is restored, and the decrypted copy is
// removed immediately.
func (s *Service) VerifyPanelBackup(ctx context.Context, uid string) error {
	if err := s.requirePanel(); err != nil {
		return err
	}
	recs, err := s.panelRepo.ListPanel(ctx)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if r.UID == uid {
			target, ok := s.targets[r.Target]
			if !ok {
				return errx.New(errx.KindUnavailable, "target_unavailable",
					"The target this backup lives on ("+r.Target+") is not configured.")
			}
			return s.verifyPanelObject(ctx, target, r.RemoteKey)
		}
	}
	return errx.NotFound("backup_not_found", "No such panel backup.")
}

// VerifyLatestPanelBackup verifies the newest panel snapshot and returns its
// UID. It returns ("", nil) when there is nothing to verify yet — an empty
// history is not a failure.
func (s *Service) VerifyLatestPanelBackup(ctx context.Context) (string, error) {
	if err := s.requirePanel(); err != nil {
		return "", err
	}
	recs, err := s.panelRepo.ListPanel(ctx)
	if err != nil {
		return "", err
	}
	if len(recs) == 0 {
		return "", nil
	}
	latest := recs[len(recs)-1] // ListPanel is oldest-first
	target, ok := s.targets[latest.Target]
	if !ok {
		return latest.UID, errx.New(errx.KindUnavailable, "target_unavailable",
			"The target this backup lives on ("+latest.Target+") is not configured.")
	}
	return latest.UID, s.verifyPanelObject(ctx, target, latest.RemoteKey)
}

// verifyPanelObject fetches a sealed object from its target, decrypts it, and
// validates the archive. Any failure means the snapshot is not restorable.
func (s *Service) verifyPanelObject(ctx context.Context, t Target, key string) error {
	sealed, err := t.Get(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = sealed.Close() }()

	// Decrypt into a transient file in staging; it holds panel data in the clear
	// for the length of this check and is removed whatever happens.
	tmp, err := os.CreateTemp(s.staging, "panelverify-*.tar.gz")
	if err != nil {
		return errx.Internal(err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := blobcrypt.Open(tmp, sealed, s.key); err != nil {
		_ = tmp.Close()
		if err == blobcrypt.ErrCorrupt {
			return errx.New(errx.KindConflict, "backup_corrupt",
				"The panel backup failed authentication — it is corrupt, was tampered with, or the master key has changed.")
		}
		return errx.Internal(err)
	}
	if err := tmp.Close(); err != nil {
		return errx.Internal(err)
	}
	return validatePanelArchive(tmpPath)
}

// validatePanelArchive confirms the decrypted file is a gzip tar carrying the
// panel manifest (with the right kind) and a database member — i.e. a real,
// complete snapshot rather than an empty or truncated one.
func validatePanelArchive(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errx.Internal(err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return errx.New(errx.KindConflict, "backup_invalid",
			"The panel backup did not decompress — it is not a valid snapshot.")
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var sawManifest, sawDB bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errx.New(errx.KindConflict, "backup_invalid",
				"The panel backup archive is truncated or corrupt.")
		}
		switch hdr.Name {
		case "manifest.json":
			var m map[string]string
			if err := json.NewDecoder(tr).Decode(&m); err != nil {
				return errx.New(errx.KindConflict, "backup_invalid",
					"The panel backup manifest is unreadable.")
			}
			if m["kind"] != panelManifestKind {
				return errx.New(errx.KindConflict, "backup_invalid",
					"The panel backup manifest is not a HeroPanel panel snapshot.")
			}
			sawManifest = true
		case "panel.db", "panel.sql.gz":
			sawDB = true
		}
	}
	if !sawManifest || !sawDB {
		return errx.New(errx.KindConflict, "backup_invalid",
			"The panel backup is incomplete — it is missing its manifest or database.")
	}
	return nil
}
