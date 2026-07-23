// Package backup is the site backup module: full + incremental archives,
// compressed (zstd, by tar in the broker), **always encrypted** (chunked
// AES-256-GCM via pkg/blobcrypt) before they touch any target, stored locally or
// on any S3-compatible endpoint, on a schedule, with restore into a fresh site.
//
// The split of trust: the broker only ever tars a validated site tree into a
// staging file and untars a staged file back (small, auditable, root). hpd —
// unprivileged — does everything else: sealing, uploading, chain bookkeeping,
// scheduling, retention. A backup is sealed with a key derived from the panel's
// master key, so a stolen bucket or disk yields ciphertext; without
// HP_SECRET_KEY the module reports unavailable rather than ever storing a
// site's data in the clear.
package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/blobcrypt"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// StagingDir is where the broker stages plain archives and hpd writes sealed
// ones; owned by the panel user, mode 0700.
const StagingDir = "/var/lib/heropanel/backups"

// Levels and targets.
const (
	LevelFull = "full"
	LevelIncr = "incr"

	TargetLocal = "local"
	TargetS3    = "s3"
)

// Backup is the API view of one archive in a chain. DBName is set when the
// backup carries a sealed database dump alongside the tree archive.
type Backup struct {
	UID       string `json:"uid"`
	Level     string `json:"level"`
	Target    string `json:"target"`
	SizeBytes int64  `json:"size_bytes"`
	DBName    string `json:"db_name,omitempty"`
	CreatedAt string `json:"created_at"`
}

// Record is the persistence view. DBKey/DBName describe the sealed database
// dump stored next to the tree archive (” = the backup carries no dump).
type Record struct {
	UID       string `db:"uid"`
	SiteID    int64  `db:"site_id"`
	Level     string `db:"level"`
	Status    string `db:"status"`
	Target    string `db:"target"`
	RemoteKey string `db:"remote_key"`
	SizeBytes int64  `db:"size_bytes"`
	DBKey     string `db:"db_key"`
	DBName    string `db:"db_name"`
	CreatedAt string `db:"created_at"`
}

// Config is a site's backup policy. DBUID optionally names one panel-managed
// database whose FULL dump rides along with every backup of the site — SQL
// dumps do not do incrementals, each stands alone.
type Config struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"interval_hours"`
	Target        string `json:"target"`
	KeepChains    int    `json:"keep_chains"`
	DBUID         string `json:"db_uid"`
}

// ConfigRow pairs a config with its site for the scheduler sweep.
type ConfigRow struct {
	SiteID int64
	Config
}

// Repo is the persistence contract.
type Repo interface {
	Insert(ctx context.Context, r *Record) error
	// ListBySiteID returns a site's backups, oldest first (chain order).
	ListBySiteID(ctx context.Context, siteID int64) ([]Record, error)
	GetByUID(ctx context.Context, uid string) (*Record, error)
	Delete(ctx context.Context, uid string) error
	GetConfig(ctx context.Context, siteID int64) (*Config, error)
	UpsertConfig(ctx context.Context, siteID int64, c Config) error
	EnabledConfigs(ctx context.Context) ([]ConfigRow, error)
}

// SiteRef is what the module needs about a site.
type SiteRef struct {
	ID        int64
	UID       string
	LinuxUser string
	HomeDir   string
}

// Sites resolves sites by UID (adapter over internal/site).
type Sites interface {
	Resolve(ctx context.Context, siteUID string) (*SiteRef, error)
}

// Target stores and fetches sealed archives by key.
type Target interface {
	Name() string
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// DBs is what the module needs from the database module (adapter over
// internal/database). Nil = no database module on this host; a config naming a
// db_uid is then refused rather than silently ignored.
type DBs interface {
	// Resolve returns the database's name for a panel database UID.
	Resolve(ctx context.Context, uid string) (string, error)
	// Export dumps the database and returns the gzipped dump's path plus the
	// database name. The caller removes the file when done.
	Export(ctx context.Context, uid string) (path, name string, err error)
	// ImportStagePath returns where a dump must be staged for a later import,
	// with the bare filename the import expects.
	ImportStagePath(gzipped bool) (path, file string)
}

// Service runs the backup pipeline.
type Service struct {
	repo    Repo
	sites   Sites
	broker  broker.Gateway
	key     []byte // 32-byte sealing key; nil = module unavailable
	staging string
	targets map[string]Target
	dbs     DBs // nil = no database module
	now     func() time.Time

	// Panel self-backup (see panel.go); nil = not wired.
	panelRepo   PanelRepo
	panelSnap   PanelSnapshotter
	panelPolicy PanelPolicy
}

// NewService constructs the service. key nil disables the module (encryption is
// not optional). The local target always exists; s3 may be nil.
func NewService(repo Repo, sites Sites, gw broker.Gateway, key []byte, s3 Target) *Service {
	targets := map[string]Target{TargetLocal: localTarget{dir: StagingDir}}
	if s3 != nil {
		targets[TargetS3] = s3
	}
	return &Service{
		repo: repo, sites: sites, broker: gw, key: key,
		staging: StagingDir, targets: targets, now: time.Now,
	}
}

// WithDBs wires the database module so backups can carry a database dump.
func (s *Service) WithDBs(d DBs) *Service { s.dbs = d; return s }

// Available reports whether backups can run at all.
func (s *Service) Available() bool {
	return s != nil && len(s.key) == 32 && s.broker != nil
}

// HasS3 reports whether the s3 target is configured.
func (s *Service) HasS3() bool { _, ok := s.targets[TargetS3]; return ok }

func (s *Service) requireAvailable() error {
	if s.Available() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "backup_unavailable",
		"Backups need the broker and a data key (HP_SECRET_KEY); encrypted-at-rest is not optional.")
}

// remoteKey names a sealed archive on its target.
func remoteKey(siteUID, backupUID string) string {
	return "sites/" + siteUID + "/" + backupUID + ".enc"
}

// remoteDBKey names the sealed database dump stored next to the tree archive.
func remoteDBKey(siteUID, backupUID string) string {
	return "sites/" + siteUID + "/" + backupUID + ".db.enc"
}

// Create runs one backup now. level empty picks automatically: full when the
// site has no chain yet, incremental otherwise.
func (s *Service) Create(ctx context.Context, siteUID, level, targetName string) (*Backup, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	existing, err := s.repo.ListBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	switch level {
	case "":
		if len(existing) == 0 {
			level = LevelFull
		} else {
			level = LevelIncr
		}
	case LevelFull, LevelIncr:
	default:
		return nil, errx.Validation("invalid_level", "Level must be full or incr.")
	}
	// An incremental needs a chain to diff against.
	if level == LevelIncr && len(existing) == 0 {
		level = LevelFull
	}
	if targetName == "" {
		targetName = TargetLocal
	}
	target, ok := s.targets[targetName]
	if !ok {
		return nil, errx.Validation("invalid_target", "That backup target is not configured.")
	}

	uid := idgen.NewULID()
	plainName := uid + ".tar.zst"

	// 1. The broker archives the tree into staging (zstd, tar-native incremental).
	if _, err := s.broker.Invoke(ctx, "backup.create", map[string]any{
		"vhost": ref.LinuxUser, "home": ref.HomeDir, "file": plainName, "level": level,
	}); err != nil {
		return nil, err
	}
	plainPath := filepath.Join(s.staging, plainName)
	// Whatever happens next, the plaintext staging file must not outlive this
	// call — it holds the site's entire tree unencrypted.
	defer func() { _ = os.Remove(plainPath) }()

	// 2. Seal it. The sealed file is what exists from here on.
	sealedPath := filepath.Join(s.staging, uid+".enc")
	size, err := s.sealFile(plainPath, sealedPath)
	if err != nil {
		return nil, errx.Internal(err)
	}

	// 2b. The database, when the site's policy names one: a FULL dump per
	// backup (SQL dumps do not do incrementals), sealed as a second object on
	// the same target. A failed dump fails the whole backup — a backup that
	// silently skipped its database would be a lie the operator discovers at
	// restore time.
	var dbKey, dbName, dbSealedPath string
	var dbSize int64
	if cfg, cerr := s.repo.GetConfig(ctx, ref.ID); cerr == nil && cfg.DBUID != "" {
		if s.dbs == nil {
			_ = os.Remove(sealedPath)
			return nil, errx.New(errx.KindUnavailable, "db_unavailable",
				"This site's backup policy includes a database, but database management is not available.")
		}
		dumpPath, name, derr := s.dbs.Export(ctx, cfg.DBUID)
		if derr != nil {
			_ = os.Remove(sealedPath)
			return nil, derr
		}
		// The plaintext dump is a full copy of the customer's data; it must not
		// outlive this call any more than the tree archive does.
		defer func() { _ = os.Remove(dumpPath) }()
		dbSealedPath = filepath.Join(s.staging, uid+".db.enc")
		dbSize, derr = s.sealFile(dumpPath, dbSealedPath)
		if derr != nil {
			_ = os.Remove(sealedPath)
			return nil, errx.Internal(derr)
		}
		dbKey, dbName = remoteDBKey(siteUID, uid), name
	}

	// 3. Hand it to the target. The local target's Put is a no-op rename-in-place;
	// a remote target uploads and the local sealed copy is removed.
	key := remoteKey(siteUID, uid)
	if err := s.storeSealed(ctx, target, key, sealedPath, size); err != nil {
		_ = os.Remove(sealedPath)
		if dbSealedPath != "" {
			_ = os.Remove(dbSealedPath)
		}
		return nil, err
	}
	if dbKey != "" {
		if err := s.storeSealed(ctx, target, dbKey, dbSealedPath, dbSize); err != nil {
			// Don't leave a tree object whose row will never exist.
			_ = target.Delete(ctx, key)
			_ = os.Remove(dbSealedPath)
			return nil, err
		}
	}

	rec := &Record{
		UID: uid, SiteID: ref.ID, Level: level, Status: "done",
		Target: targetName, RemoteKey: key, SizeBytes: size,
		DBKey: dbKey, DBName: dbName,
	}
	if err := s.repo.Insert(ctx, rec); err != nil {
		return nil, err
	}
	// Starting a new chain retires old ones beyond the retention policy.
	if level == LevelFull {
		s.pruneChains(ctx, ref, siteUID)
	}
	return toView(rec), nil
}

// sealFile seals src into dst and returns dst's size.
func (s *Service) sealFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	if err := blobcrypt.Seal(out, in, s.key); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	st, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// storeSealed moves the sealed file onto its target.
func (s *Service) storeSealed(ctx context.Context, t Target, key, sealedPath string, size int64) error {
	if t.Name() == TargetLocal {
		// Local: the sealed staging file *is* the stored object; rename it under
		// the key's basename so Get can find it.
		return os.Rename(sealedPath, filepath.Join(s.staging, filepath.Base(key)))
	}
	f, err := os.Open(sealedPath)
	if err != nil {
		return errx.Internal(err)
	}
	defer func() { _ = f.Close() }()
	if err := t.Put(ctx, key, f, size); err != nil {
		return err
	}
	return os.Remove(sealedPath)
}

// List returns a site's backups (newest first for display) plus its config.
func (s *Service) List(ctx context.Context, siteUID string) ([]Backup, *Config, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, nil, err
	}
	recs, err := s.repo.ListBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Backup, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		out = append(out, *toView(&recs[i]))
	}
	cfg, err := s.repo.GetConfig(ctx, ref.ID)
	if err != nil {
		return nil, nil, err
	}
	return out, cfg, nil
}

// SetConfig stores a site's backup policy.
func (s *Service) SetConfig(ctx context.Context, siteUID string, c Config) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return err
	}
	if c.IntervalHours < 1 || c.IntervalHours > 24*30 {
		return errx.Validation("invalid_interval", "The interval must be between 1 hour and 30 days.")
	}
	if c.KeepChains < 1 || c.KeepChains > 30 {
		return errx.Validation("invalid_retention", "keep_chains must be between 1 and 30.")
	}
	if _, ok := s.targets[c.Target]; !ok {
		return errx.Validation("invalid_target", "That backup target is not configured.")
	}
	if c.DBUID != "" {
		if s.dbs == nil {
			return errx.Validation("db_unavailable", "Database management is not available on this host.")
		}
		// Refuse a policy naming a database that does not exist — the failure
		// would otherwise surface only when the next scheduled backup runs.
		if _, err := s.dbs.Resolve(ctx, c.DBUID); err != nil {
			return errx.Validation("invalid_db", "That database does not exist.")
		}
	}
	return s.repo.UpsertConfig(ctx, ref.ID, c)
}

// chainFor returns the restore chain ending at target: the latest full at or
// before it, then every backup after that full up to and including it.
func chainFor(all []Record, targetUID string) ([]Record, error) {
	idx := -1
	for i := range all {
		if all[i].UID == targetUID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, errx.NotFound("backup_not_found", "No such backup.")
	}
	start := -1
	for i := idx; i >= 0; i-- {
		if all[i].Level == LevelFull {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, errx.New(errx.KindConflict, "broken_chain",
			"This backup's chain has no full backup — it cannot be restored.")
	}
	return all[start : idx+1], nil
}

// Restore replays the chain ending at backupUID into the (already provisioned)
// site destUID. The caller creates the destination first — restoring into a
// *new* site is exactly that, orchestrated by the HTTP layer.
func (s *Service) Restore(ctx context.Context, siteUID, backupUID, destUID string) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	srcRef, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return err
	}
	destRef, err := s.sites.Resolve(ctx, destUID)
	if err != nil {
		return err
	}
	all, err := s.repo.ListBySiteID(ctx, srcRef.ID)
	if err != nil {
		return err
	}
	chain, err := chainFor(all, backupUID)
	if err != nil {
		return err
	}

	for i := range chain {
		if err := s.restoreOne(ctx, srcRef, destRef, &chain[i]); err != nil {
			return err
		}
	}
	return nil
}

// restoreOne fetches, opens and extracts a single archive of the chain.
func (s *Service) restoreOne(ctx context.Context, srcRef, destRef *SiteRef, rec *Record) error {
	target, ok := s.targets[rec.Target]
	if !ok {
		return errx.New(errx.KindUnavailable, "target_unavailable",
			"The target this backup lives on ("+rec.Target+") is not configured.")
	}
	sealed, err := target.Get(ctx, rec.RemoteKey)
	if err != nil {
		return err
	}
	defer func() { _ = sealed.Close() }()

	plainName := rec.UID + ".tar.zst"
	plainPath := filepath.Join(s.staging, plainName)
	out, err := os.OpenFile(plainPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return errx.Internal(err)
	}
	// The decrypted archive is transient; remove it whatever happens.
	defer func() { _ = os.Remove(plainPath) }()
	if err := blobcrypt.Open(out, sealed, s.key); err != nil {
		_ = out.Close()
		if err == blobcrypt.ErrCorrupt {
			return errx.New(errx.KindConflict, "backup_corrupt",
				"The backup failed authentication — it is corrupt or was tampered with, and was not restored.")
		}
		return errx.Internal(err)
	}
	if err := out.Close(); err != nil {
		return errx.Internal(err)
	}

	_, err = s.broker.Invoke(ctx, "backup.restore", map[string]any{
		"home": destRef.HomeDir, "username": destRef.LinuxUser, "file": plainName,
	})
	return err
}

// StageDBDump fetches a backup's sealed database dump, opens it, and stages
// the plaintext gzipped dump where the database module's import expects it.
// It returns the staged file's absolute path (for cleanup on an abandoned
// restore), the bare filename to pass to Import, and the database's original
// name. The dump itself never touches a site tree — it goes DumpDir-to-import
// under panel-only permissions.
func (s *Service) StageDBDump(ctx context.Context, siteUID, backupUID string) (path, file, dbName string, err error) {
	if err := s.requireAvailable(); err != nil {
		return "", "", "", err
	}
	if s.dbs == nil {
		return "", "", "", errx.New(errx.KindUnavailable, "db_unavailable",
			"Database management is not available on this host.")
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return "", "", "", err
	}
	rec, err := s.repo.GetByUID(ctx, backupUID)
	if err != nil {
		return "", "", "", err
	}
	if rec.SiteID != ref.ID {
		return "", "", "", errx.NotFound("backup_not_found", "No such backup.")
	}
	if rec.DBKey == "" {
		return "", "", "", errx.NotFound("no_db_dump", "This backup carries no database dump.")
	}
	target, ok := s.targets[rec.Target]
	if !ok {
		return "", "", "", errx.New(errx.KindUnavailable, "target_unavailable",
			"The target this backup lives on ("+rec.Target+") is not configured.")
	}
	sealed, err := target.Get(ctx, rec.DBKey)
	if err != nil {
		return "", "", "", err
	}
	defer func() { _ = sealed.Close() }()

	path, file = s.dbs.ImportStagePath(true)
	// Best-effort: the dump directory normally exists (the dump was exported
	// through it), but a restore on a fresh host may precede any export.
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", "", errx.Internal(err)
	}
	if err := blobcrypt.Open(out, sealed, s.key); err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		if err == blobcrypt.ErrCorrupt {
			return "", "", "", errx.New(errx.KindConflict, "backup_corrupt",
				"The database dump failed authentication — it is corrupt or was tampered with, and was not staged.")
		}
		return "", "", "", errx.Internal(err)
	}
	if err := out.Close(); err != nil {
		return "", "", "", errx.Internal(err)
	}
	return path, file, rec.DBName, nil
}

// Delete removes a backup — and, because later incrementals depend on it, every
// later backup in the same chain. Said plainly rather than silently breaking a
// chain that would only be discovered at restore time.
func (s *Service) Delete(ctx context.Context, siteUID, backupUID string) ([]string, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	all, err := s.repo.ListBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i := range all {
		if all[i].UID == backupUID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, errx.NotFound("backup_not_found", "No such backup.")
	}
	// The victim and everything after it until the next full (the dependents).
	end := len(all)
	for i := idx + 1; i < len(all); i++ {
		if all[i].Level == LevelFull {
			end = i
			break
		}
	}
	removed := []string{}
	for i := idx; i < end; i++ {
		s.deleteStored(ctx, &all[i])
		if err := s.repo.Delete(ctx, all[i].UID); err != nil {
			return removed, err
		}
		removed = append(removed, all[i].UID)
	}
	return removed, nil
}

// deleteStored best-effort removes a backup's sealed objects — the tree
// archive and, when it carried one, the database dump — from its target.
func (s *Service) deleteStored(ctx context.Context, rec *Record) {
	if target, ok := s.targets[rec.Target]; ok {
		_ = target.Delete(ctx, rec.RemoteKey)
		if rec.DBKey != "" {
			_ = target.Delete(ctx, rec.DBKey)
		}
	}
}

// pruneChains enforces keep_chains after a new full begins a chain.
func (s *Service) pruneChains(ctx context.Context, ref *SiteRef, siteUID string) {
	cfg, err := s.repo.GetConfig(ctx, ref.ID)
	if err != nil || cfg.KeepChains < 1 {
		return
	}
	all, err := s.repo.ListBySiteID(ctx, ref.ID)
	if err != nil {
		return
	}
	// Index the start of each chain.
	var starts []int
	for i := range all {
		if all[i].Level == LevelFull {
			starts = append(starts, i)
		}
	}
	excess := len(starts) - cfg.KeepChains
	if excess <= 0 {
		return
	}
	// Everything before the first kept chain goes.
	cutoff := starts[excess]
	for i := 0; i < cutoff; i++ {
		s.deleteStored(ctx, &all[i])
		_ = s.repo.Delete(ctx, all[i].UID)
	}
}

// RunScheduler sweeps hourly: every enabled site whose newest backup is older
// than its interval gets one (auto level). It is hpd's own ticker — like the SSL
// renewer — rather than a cron unit, because it needs the panel's key and DB.
func (s *Service) RunScheduler(ctx context.Context, sitesByID func(ctx context.Context, id int64) (string, bool), log interface{ Info(string, ...any) }) {
	if !s.Available() {
		return
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweepDue(ctx, sitesByID, log)
		}
	}
}

// sweepDue runs one scheduler pass.
func (s *Service) sweepDue(ctx context.Context, siteUIDByID func(ctx context.Context, id int64) (string, bool), log interface{ Info(string, ...any) }) {
	rows, err := s.repo.EnabledConfigs(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		uid, ok := siteUIDByID(ctx, row.SiteID)
		if !ok {
			continue
		}
		recs, err := s.repo.ListBySiteID(ctx, row.SiteID)
		if err != nil {
			continue
		}
		due := true
		if len(recs) > 0 {
			if ts, err := time.Parse("2006-01-02 15:04:05", recs[len(recs)-1].CreatedAt); err == nil {
				due = s.now().UTC().Sub(ts) >= time.Duration(row.IntervalHours)*time.Hour
			}
		}
		if !due {
			continue
		}
		if _, err := s.Create(ctx, uid, "", row.Target); err == nil && log != nil {
			log.Info("scheduled backup completed", "site", uid)
		}
	}
}

func toView(r *Record) *Backup {
	return &Backup{
		UID: r.UID, Level: r.Level, Target: r.Target,
		SizeBytes: r.SizeBytes, DBName: r.DBName, CreatedAt: r.CreatedAt,
	}
}

// ── local target ─────────────────────────────────────────────────────────────

// localTarget stores sealed archives as files in the staging directory. Put is
// unused for local (storeSealed renames in place); Get/Delete address the file
// by the key's basename.
type localTarget struct{ dir string }

func (localTarget) Name() string { return TargetLocal }

func (l localTarget) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	f, err := os.OpenFile(filepath.Join(l.dir, filepath.Base(key)), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (l localTarget) Get(_ context.Context, key string) (io.ReadCloser, error) {
	name := filepath.Base(key)
	if strings.Contains(name, "..") {
		return nil, errx.Validation("invalid_key", "Invalid backup key.")
	}
	f, err := os.Open(filepath.Join(l.dir, name))
	if err != nil {
		return nil, errx.NotFound("backup_missing", "The backup file is missing from local storage.")
	}
	return f, nil
}

func (l localTarget) Delete(_ context.Context, key string) error {
	return os.Remove(filepath.Join(l.dir, filepath.Base(key)))
}
