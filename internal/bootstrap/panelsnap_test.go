package bootstrap

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/repository"
)

// The SQLite snapshotter produces a tar.gz holding a real VACUUM'd copy of the
// live database plus a manifest — proven against an actual SQLite handle, not
// a mock: the copy must open as a database and contain the row.
func TestPanelSnapshotterSQLite(t *testing.T) {
	dir := t.TempDir()
	dbCfg := config.Database{Driver: "sqlite", DSN: filepath.Join(dir, "panel.db")}
	db, err := repository.Open(dbCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO t (v) VALUES ('survives the snapshot')`); err != nil {
		t.Fatal(err)
	}

	snap := panelSnapshotter(db, dbCfg, nil)
	out, err := snap(ctx, dir)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	defer func() { _ = os.Remove(out) }()

	entries := untarGz(t, out, dir)
	manifest, ok := entries["manifest.json"]
	if !ok {
		t.Fatal("no manifest in the snapshot")
	}
	var m map[string]string
	if err := json.Unmarshal(manifest, &m); err != nil || m["driver"] != "sqlite" || m["kind"] != "heropanel-panel-backup" {
		t.Errorf("manifest = %s, %v", manifest, err)
	}
	dbBytes, ok := entries["panel.db"]
	if !ok || len(dbBytes) == 0 {
		t.Fatal("no panel.db in the snapshot")
	}
	// The snapshot must be a working database with the row, not just bytes.
	copyPath := filepath.Join(dir, "restored.db")
	if err := os.WriteFile(copyPath, dbBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	db2, err := repository.Open(config.Database{Driver: "sqlite", DSN: copyPath})
	if err != nil {
		t.Fatalf("the snapshot does not open as a database: %v", err)
	}
	defer func() { _ = db2.Close() }()
	var v string
	if err := db2.GetContext(ctx, &v, `SELECT v FROM t`); err != nil || v != "survives the snapshot" {
		t.Errorf("row in snapshot = %q, %v", v, err)
	}
	// The VACUUM working directory must not linger.
	if m, _ := filepath.Glob(filepath.Join(dir, "panelsnap-*")); len(m) != 0 {
		t.Errorf("snapshot working dir leaked: %v", m)
	}
}

func untarGz(t *testing.T, path, _ string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = b
	}
	return out
}

// mysqlDBName pulls the schema name out of a go-sql-driver DSN.
func TestMySQLDBName(t *testing.T) {
	cases := map[string]string{
		"hp:pw@tcp(127.0.0.1:3306)/heropanel?parseTime=true": "heropanel",
		"hp:pw@unix(/run/mysqld.sock)/panel":                 "panel",
	}
	for dsn, want := range cases {
		got, err := mysqlDBName(dsn)
		if err != nil || got != want {
			t.Errorf("mysqlDBName(%q) = %q, %v; want %q", dsn, got, err, want)
		}
	}
	for _, dsn := range []string{"", "hp:pw@tcp(host)/", "no-slash"} {
		if _, err := mysqlDBName(dsn); err == nil {
			t.Errorf("mysqlDBName(%q) succeeded", dsn)
		}
	}
}
