package bootstrap

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// panelSnapshotter builds the dialect-appropriate snapshot function for panel
// self-backup: a plaintext .tar.gz in the staging dir holding the panel's
// database plus a small manifest. The backup service seals it and removes the
// plaintext — this function's product never leaves the panel-owned staging
// directory unencrypted for longer than one Create call.
//
// SQLite snapshots with VACUUM INTO on the live handle (a consistent copy
// without stopping the panel — the documented online-backup idiom). MariaDB
// goes through the broker's db.export (mysqldump --single-transaction), the
// same capability customer databases use.
func panelSnapshotter(db *repository.DB, dbCfg config.Database, gw broker.Gateway) func(ctx context.Context, dir string) (string, error) {
	return func(ctx context.Context, dir string) (string, error) {
		work, err := os.MkdirTemp(dir, "panelsnap-")
		if err != nil {
			return "", err
		}
		defer func() { _ = os.RemoveAll(work) }()

		var dbFile string
		switch db.Dialect {
		case repository.DialectSQLite:
			dbFile = filepath.Join(work, "panel.db")
			// The path is panel-generated (no user input); quotes doubled anyway.
			q := strings.ReplaceAll(dbFile, "'", "''")
			if _, err := db.ExecContext(ctx, "VACUUM INTO '"+q+"'"); err != nil {
				return "", fmt.Errorf("panel snapshot: vacuum: %w", err)
			}
		case repository.DialectMySQL:
			if gw == nil {
				return "", fmt.Errorf("panel snapshot: mariadb needs the broker for mysqldump")
			}
			name, err := mysqlDBName(dbCfg.DSN)
			if err != nil {
				return "", err
			}
			file := "panel-" + idgen.NewULID() + ".sql"
			res, err := gw.Invoke(ctx, "db.export", map[string]any{"name": name, "file": file})
			if err != nil {
				return "", fmt.Errorf("panel snapshot: export: %w", err)
			}
			dumpPath, _ := res["path"].(string)
			if dumpPath == "" {
				return "", fmt.Errorf("panel snapshot: export produced no file")
			}
			// The dump is panel-owned (db.export hands it over); pull it into the
			// working dir and never leave the original behind.
			dbFile = filepath.Join(work, "panel.sql.gz")
			if err := moveFile(dumpPath, dbFile); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("panel snapshot: unsupported dialect %q", db.Dialect)
		}

		manifest, err := json.Marshal(map[string]string{
			"kind":       "heropanel-panel-backup",
			"driver":     string(db.Dialect),
			"created_at": time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return "", err
		}
		manifestFile := filepath.Join(work, "manifest.json")
		if err := os.WriteFile(manifestFile, manifest, 0o600); err != nil {
			return "", err
		}

		out := filepath.Join(dir, "panel-"+idgen.NewULID()+".tar.gz")
		if err := tarGz(out, work, []string{filepath.Base(dbFile), "manifest.json"}); err != nil {
			_ = os.Remove(out)
			return "", err
		}
		return out, nil
	}
}

// mysqlDBName extracts the database name from a go-sql-driver DSN
// (user:pass@tcp(host)/name?params).
func mysqlDBName(dsn string) (string, error) {
	i := strings.LastIndex(dsn, "/")
	if i < 0 {
		return "", fmt.Errorf("panel snapshot: cannot find database name in DSN")
	}
	name := dsn[i+1:]
	if j := strings.Index(name, "?"); j >= 0 {
		name = name[:j]
	}
	if name == "" {
		return "", fmt.Errorf("panel snapshot: DSN names no database")
	}
	return name, nil
}

// moveFile moves src to dst, falling back to copy+remove across filesystems.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// tarGz writes the named files from dir into a gzipped tarball at out.
// Stdlib tar+gzip: the snapshot is a handful of small panel-owned files; no
// external binary and no broker round-trip is warranted.
func tarGz(out, dir string, names []string) error {
	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		path := filepath.Join(dir, name)
		st, err := os.Stat(path)
		if err != nil {
			return closeAll(err, tw, gz, f)
		}
		hdr := &tar.Header{Name: name, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			return closeAll(err, tw, gz, f)
		}
		in, err := os.Open(path)
		if err != nil {
			return closeAll(err, tw, gz, f)
		}
		_, cerr := io.Copy(tw, in)
		_ = in.Close()
		if cerr != nil {
			return closeAll(cerr, tw, gz, f)
		}
	}
	if err := tw.Close(); err != nil {
		return closeAll(err, gz, f)
	}
	if err := gz.Close(); err != nil {
		return closeAll(err, f)
	}
	return f.Close()
}

// closeAll closes writers best-effort and returns the original error.
func closeAll(err error, closers ...io.Closer) error {
	for _, c := range closers {
		_ = c.Close()
	}
	return err
}
