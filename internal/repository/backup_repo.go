package repository

import (
	"context"
	"time"

	"github.com/thisisnkp/heropanel/internal/backup"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// BackupStore implements backup.Repo over the datastore.
type BackupStore struct {
	db *DB
}

// NewBackupStore constructs a BackupStore.
func NewBackupStore(db *DB) *BackupStore { return &BackupStore{db: db} }

var _ backup.Repo = (*BackupStore)(nil)

const backupSelect = `SELECT uid, site_id, level, status, target, remote_key, size_bytes, db_key, db_name, created_at FROM backups`

// Insert records one archive.
func (s *BackupStore) Insert(ctx context.Context, r *backup.Record) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO backups (uid, site_id, level, status, target, remote_key, size_bytes, db_key, db_name, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UID, r.SiteID, r.Level, r.Status, r.Target, r.RemoteKey, r.SizeBytes, r.DBKey, r.DBName, fmtTS(time.Now()))
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ListBySiteID returns a site's backups oldest-first — chain order.
func (s *BackupStore) ListBySiteID(ctx context.Context, siteID int64) ([]backup.Record, error) {
	var rows []backup.Record
	if err := s.db.SelectContext(ctx, &rows,
		backupSelect+` WHERE site_id = ? ORDER BY created_at ASC, id ASC`, siteID); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// GetByUID returns one backup.
func (s *BackupStore) GetByUID(ctx context.Context, uid string) (*backup.Record, error) {
	var rec backup.Record
	err := s.db.GetContext(ctx, &rec, backupSelect+` WHERE uid = ?`, uid)
	if isNoRows(err) {
		return nil, errx.NotFound("backup_not_found", "No such backup.")
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &rec, nil
}

// Delete removes a backup row.
func (s *BackupStore) Delete(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM backups WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// GetConfig returns a site's backup policy; a site never configured gets the
// defaults with enabled=false.
func (s *BackupStore) GetConfig(ctx context.Context, siteID int64) (*backup.Config, error) {
	var row struct {
		Enabled       int    `db:"enabled"`
		IntervalHours int    `db:"interval_hours"`
		Target        string `db:"target"`
		KeepChains    int    `db:"keep_chains"`
		DBUID         string `db:"db_uid"`
	}
	err := s.db.GetContext(ctx, &row,
		`SELECT enabled, interval_hours, target, keep_chains, db_uid FROM backup_configs WHERE site_id = ?`, siteID)
	if isNoRows(err) {
		return &backup.Config{Enabled: false, IntervalHours: 24, Target: backup.TargetLocal, KeepChains: 2}, nil
	}
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &backup.Config{
		Enabled: row.Enabled == 1, IntervalHours: row.IntervalHours,
		Target: row.Target, KeepChains: row.KeepChains, DBUID: row.DBUID,
	}, nil
}

// UpsertConfig writes a site's backup policy (portable update-then-insert).
func (s *BackupStore) UpsertConfig(ctx context.Context, siteID int64, c backup.Config) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE backup_configs SET enabled = ?, interval_hours = ?, target = ?, keep_chains = ?, db_uid = ?, updated_at = ?
		 WHERE site_id = ?`,
		boolToInt(c.Enabled), c.IntervalHours, c.Target, c.KeepChains, c.DBUID, fmtTS(time.Now()), siteID)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_configs (site_id, enabled, interval_hours, target, keep_chains, db_uid) VALUES (?, ?, ?, ?, ?, ?)`,
		siteID, boolToInt(c.Enabled), c.IntervalHours, c.Target, c.KeepChains, c.DBUID); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// InsertPanel records one panel snapshot.
func (s *BackupStore) InsertPanel(ctx context.Context, r *backup.PanelRecord) error {
	if r.UID == "" {
		r.UID = idgen.NewULID()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = fmtTS(time.Now())
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO panel_backups (uid, target, remote_key, size_bytes, created_at) VALUES (?, ?, ?, ?, ?)`,
		r.UID, r.Target, r.RemoteKey, r.SizeBytes, r.CreatedAt); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ListPanel returns panel snapshots oldest first.
func (s *BackupStore) ListPanel(ctx context.Context) ([]backup.PanelRecord, error) {
	var rows []backup.PanelRecord
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT uid, target, remote_key, size_bytes, created_at FROM panel_backups ORDER BY created_at ASC, id ASC`); err != nil {
		return nil, errx.Internal(err)
	}
	return rows, nil
}

// DeletePanel removes a panel snapshot row.
func (s *BackupStore) DeletePanel(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM panel_backups WHERE uid = ?`, uid); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// EnabledConfigs returns every enabled policy for the scheduler sweep.
func (s *BackupStore) EnabledConfigs(ctx context.Context) ([]backup.ConfigRow, error) {
	var rows []struct {
		SiteID        int64  `db:"site_id"`
		IntervalHours int    `db:"interval_hours"`
		Target        string `db:"target"`
		KeepChains    int    `db:"keep_chains"`
		DBUID         string `db:"db_uid"`
	}
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT site_id, interval_hours, target, keep_chains, db_uid FROM backup_configs WHERE enabled = 1`); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]backup.ConfigRow, len(rows))
	for i, r := range rows {
		out[i] = backup.ConfigRow{SiteID: r.SiteID, Config: backup.Config{
			Enabled: true, IntervalHours: r.IntervalHours, Target: r.Target, KeepChains: r.KeepChains, DBUID: r.DBUID,
		}}
	}
	return out, nil
}
