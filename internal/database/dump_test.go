package database_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// seed creates a database + user and returns their UIDs.
func seed(t *testing.T, svc *database.Service) (dbUID, userUID string) {
	t.Helper()
	ctx := context.Background()
	dbi, err := svc.CreateDatabase(ctx, 1, "acme_db")
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	usr, err := svc.CreateUser(ctx, 1, "acme", "localhost", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return dbi.UID, usr.UID
}

func TestSizeReportsBytesAndTables(t *testing.T) {
	svc, gw := newSvc(t)
	gw.results = map[string]map[string]any{
		// Numbers arrive as float64 over the JSON transport.
		"db.size": {"bytes": float64(1048576), "tables": float64(12)},
	}
	dbUID, _ := seed(t, svc)

	size, err := svc.Size(context.Background(), dbUID)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if size.Bytes != 1048576 || size.Tables != 12 {
		t.Fatalf("size = %+v", size)
	}
	if c := gw.last("db.size"); c == nil || c.input["name"] != "acme_db" {
		t.Fatalf("db.size input = %+v", c)
	}
}

func TestRevokeDropsTheGrantOnlyAfterMariaDBAgrees(t *testing.T) {
	svc, gw := newSvc(t)
	ctx := context.Background()
	dbUID, userUID := seed(t, svc)
	if err := svc.Grant(ctx, dbUID, userUID, []string{"SELECT"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// A broker failure must not leave the panel claiming access was removed.
	gw.failOn = "db.revoke"
	if err := svc.Revoke(ctx, dbUID, userUID, []string{"SELECT"}); err == nil {
		t.Fatal("a failing revoke should surface an error")
	}

	gw.failOn = ""
	if err := svc.Revoke(ctx, dbUID, userUID, []string{"SELECT"}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	c := gw.last("db.revoke")
	if c == nil || c.input["database"] != "acme_db" || c.input["username"] != "acme" {
		t.Fatalf("db.revoke input = %+v", c)
	}
	privs, _ := c.input["privileges"].([]string)
	if len(privs) != 1 || privs[0] != "SELECT" {
		t.Fatalf("privileges = %+v", c.input["privileges"])
	}
}

func TestRevokeDefaultsToAll(t *testing.T) {
	svc, gw := newSvc(t)
	ctx := context.Background()
	dbUID, userUID := seed(t, svc)
	if err := svc.Revoke(ctx, dbUID, userUID, nil); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	privs, _ := gw.last("db.revoke").input["privileges"].([]string)
	if len(privs) != 1 || privs[0] != "ALL" {
		t.Fatalf("privileges = %+v", privs)
	}
}

func TestDeleteUserDropsItFromMariaDBAndTheStore(t *testing.T) {
	svc, gw := newSvc(t)
	ctx := context.Background()
	_, userUID := seed(t, svc)

	if err := svc.DeleteUser(ctx, userUID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	c := gw.last("db.user.drop")
	if c == nil || c.input["username"] != "acme" || c.input["host"] != "localhost" {
		t.Fatalf("db.user.drop input = %+v", c)
	}
	users, _ := svc.ListUsers(ctx, 0, 50, 0)
	if len(users) != 0 {
		t.Fatalf("user record survived the drop: %+v", users)
	}
	// A second delete is a clean not-found, not a crash.
	if err := svc.DeleteUser(ctx, userUID); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
}

// The record must not disappear if MariaDB refused to drop the user — otherwise
// the panel forgets about an account that still has live access.
func TestDeleteUserKeepsTheRecordWhenTheDropFails(t *testing.T) {
	svc, gw := newSvc(t)
	ctx := context.Background()
	_, userUID := seed(t, svc)

	gw.failOn = "db.user.drop"
	if err := svc.DeleteUser(ctx, userUID); err == nil {
		t.Fatal("a failing drop should surface an error")
	}
	users, _ := svc.ListUsers(ctx, 0, 50, 0)
	if len(users) != 1 {
		t.Fatal("the record was removed even though MariaDB still has the user")
	}
}

func TestExportProducesAUniqueFilePerCall(t *testing.T) {
	svc, gw := newSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)
	gw.results = map[string]map[string]any{
		"db.export": {"path": "/var/lib/heropanel/dumps/x.sql.gz", "bytes": float64(4096)},
	}

	first, err := svc.Export(ctx, dbUID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if first.Bytes != 4096 || first.Name != "acme_db" {
		t.Fatalf("export = %+v", first)
	}
	if !strings.HasPrefix(first.File, "acme_db-") || !strings.HasSuffix(first.File, ".sql.gz") {
		t.Fatalf("download filename = %q", first.File)
	}

	second, err := svc.Export(ctx, dbUID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// Two exports running at once must not write over each other's dump.
	if first.File == second.File {
		t.Fatalf("two exports reused the same filename: %q", first.File)
	}
}

func TestExportWithoutAPathIsAnError(t *testing.T) {
	svc, gw := newSvc(t)
	dbUID, _ := seed(t, svc)
	gw.results = map[string]map[string]any{"db.export": {"bytes": float64(0)}}
	if _, err := svc.Export(context.Background(), dbUID); err == nil {
		t.Fatal("an export with no file should not report success")
	}
}

func TestDiscardExportRemovesTheDump(t *testing.T) {
	svc, _ := newSvc(t)
	f, err := os.CreateTemp(t.TempDir(), "dump-*.sql.gz")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	path := f.Name()
	_ = f.Close()

	if err := svc.DiscardExport(path); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the dump was left on disk")
	}
	// An empty path is a no-op, not an error (the export may have failed early).
	if err := svc.DiscardExport(""); err != nil {
		t.Fatalf("discard(\"\"): %v", err)
	}
}

func TestImportStagePathIsUniqueAndTracksCompression(t *testing.T) {
	svc, _ := newSvc(t)

	plainPath, plainFile := svc.ImportStagePath(false)
	if !strings.HasSuffix(plainFile, ".sql") || strings.HasSuffix(plainFile, ".gz") {
		t.Fatalf("plain staging file = %q", plainFile)
	}
	if plainPath != database.DumpDir+"/"+plainFile {
		t.Fatalf("staging path = %q", plainPath)
	}

	gzPath, gzFile := svc.ImportStagePath(true)
	if !strings.HasSuffix(gzFile, ".sql.gz") {
		t.Fatalf("gzipped staging file = %q", gzFile)
	}
	_ = gzPath
	if plainFile == gzFile {
		t.Fatal("staging filenames are not unique")
	}
}

func TestImportPassesTheStagedFileToTheBroker(t *testing.T) {
	svc, gw := newSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	_, file := svc.ImportStagePath(true)
	if err := svc.Import(ctx, dbUID, file); err != nil {
		t.Fatalf("import: %v", err)
	}
	c := gw.last("db.import")
	if c == nil || c.input["name"] != "acme_db" || c.input["file"] != file {
		t.Fatalf("db.import input = %+v", c)
	}
}

func TestDumpOpsRejectUnknownDatabases(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	for name, err := range map[string]error{
		"size":   func() error { _, e := svc.Size(ctx, "nope"); return e }(),
		"export": func() error { _, e := svc.Export(ctx, "nope"); return e }(),
		"import": svc.Import(ctx, "nope", "x.sql"),
	} {
		if !errx.IsKind(err, errx.KindNotFound) {
			t.Fatalf("%s: want not_found, got %v", name, err)
		}
	}
}
