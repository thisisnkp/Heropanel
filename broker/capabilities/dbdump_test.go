package capabilities_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func dumpCtx(r exec.Runner, f *fsys.Fake) capability.Context {
	return capability.Context{Ctx: context.Background(), Runner: r, Policy: policy.Default(), FS: f}
}

func TestDBSizeReportsBytesAndTables(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(exec.Command) (exec.Result, error) {
		return exec.Result{Stdout: []byte("1048576\t12\n")}, nil
	}}
	res, err := (capabilities.DBSize{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"name": "acme_db"}))
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if res.Data["bytes"] != int64(1048576) || res.Data["tables"] != int64(12) {
		t.Fatalf("result = %+v", res.Data)
	}
	// The query goes in over stdin, never argv.
	if len(fr.Calls) != 1 || len(fr.Calls[0].Stdin) == 0 {
		t.Fatalf("query not piped over stdin: %+v", fr.Calls)
	}
	if !strings.Contains(string(fr.Calls[0].Stdin), "information_schema.TABLES") {
		t.Fatalf("unexpected query: %s", fr.Calls[0].Stdin)
	}
}

// An empty database is 0 bytes, not an error.
func TestDBSizeHandlesAnEmptyDatabase(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(exec.Command) (exec.Result, error) {
		return exec.Result{Stdout: []byte("0\t0\n")}, nil
	}}
	res, err := (capabilities.DBSize{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"name": "empty_db"}))
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if res.Data["bytes"] != int64(0) || res.Data["tables"] != int64(0) {
		t.Fatalf("result = %+v", res.Data)
	}
}

func TestDBExportDumpsToAFileAndHandsItToThePanel(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if hasToken(cmd, "-c") && hasToken(cmd, "%s") {
			return exec.Result{Stdout: []byte("4096\n")}, nil
		}
		return exec.Result{}, nil
	}}
	res, err := (capabilities.DBExport{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"name": "acme_db", "file": "acme_db-01H.sql"}))
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.Data["path"] != "/var/lib/heropanel/dumps/acme_db-01H.sql.gz" || res.Data["bytes"] != int64(4096) {
		t.Fatalf("result = %+v", res.Data)
	}

	// The dump is written straight to a file: nothing this large should ever be
	// buffered through the broker's memory or its JSON transport.
	dump, ok := findCall(fr.Calls, "/usr/bin/mysqldump")
	if !ok {
		t.Fatalf("mysqldump did not run; calls=%+v", fr.Calls)
	}
	if !hasToken(dump, "--result-file=/var/lib/heropanel/dumps/acme_db-01H.sql") {
		t.Fatalf("dump was not written to a file: %+v", dump.Args)
	}
	// --single-transaction is what keeps an export from locking a live site out.
	for _, flag := range []string{"--single-transaction", "--quick", "--routines", "--triggers"} {
		if !hasToken(dump, flag) {
			t.Fatalf("mysqldump missing %s: %+v", flag, dump.Args)
		}
	}
	if _, ok := findCall(fr.Calls, "/usr/bin/gzip", "-f", "/var/lib/heropanel/dumps/acme_db-01H.sql"); !ok {
		t.Fatalf("dump was not compressed; calls=%+v", fr.Calls)
	}

	// The dump directory belongs to the panel user: hpd has to traverse it to
	// stream the export back, and a 0700 root-owned directory would lock it out.
	if _, ok := findCall(fr.Calls, "/usr/bin/install", "-d", "-m", "0700",
		"-o", "heropanel", "-g", "heropanel", "/var/lib/heropanel/dumps"); !ok {
		t.Fatalf("dump directory was not created for the panel user; calls=%+v", fr.Calls)
	}

	// 0600 + owned by the panel user. mysqldump writes under root's umask (0644),
	// which would leave one customer's whole database readable by every other
	// site user on the box.
	chmod, ok := findCall(fr.Calls, "/bin/chmod", "0600", "/var/lib/heropanel/dumps/acme_db-01H.sql.gz")
	if !ok {
		t.Fatalf("dump was not restricted to 0600; calls=%+v", fr.Calls)
	}
	_ = chmod
	if _, ok := findCall(fr.Calls, "/bin/chown", "heropanel:heropanel",
		"/var/lib/heropanel/dumps/acme_db-01H.sql.gz"); !ok {
		t.Fatalf("dump was not handed to the panel user; calls=%+v", fr.Calls)
	}
}

func TestDBExportCleansUpWhenTheDumpFails(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if hasToken(cmd, "/usr/bin/mysqldump") {
			return exec.Result{ExitCode: 2, Stderr: []byte("Unknown database")}, nil
		}
		return exec.Result{}, nil
	}}
	f := fsys.NewFake()
	if _, err := (capabilities.DBExport{}).Execute(dumpCtx(fr, f),
		raw(t, map[string]any{"name": "acme_db", "file": "x.sql"})); !errx.IsKind(err, errx.KindUpstream) {
		t.Fatalf("want upstream, got %v", err)
	}
	// A failed dump must not be compressed and served as if it were real data.
	if _, ok := findCall(fr.Calls, "/usr/bin/gzip"); ok {
		t.Fatal("a failed dump was compressed anyway")
	}
	if _, ok := findCall(fr.Calls, "/bin/chown"); ok {
		t.Fatal("a failed dump was handed to the panel")
	}
}

// The filename becomes a path, so it is the injection surface here.
func TestDBExportRejectsPathsInTheFilename(t *testing.T) {
	for name, file := range map[string]string{
		"traversal":      "../../etc/passwd.sql",
		"absolute":       "/etc/shadow.sql",
		"subdirectory":   "sub/dir.sql",
		"no extension":   "dump",
		"wrong ext":      "dump.tar",
		"already gzip":   "dump.sql.gz",
		"leading dash":   "-rf.sql",
		"empty":          "",
		"hidden dotfile": ".bashrc.sql",
	} {
		fr := &exec.FakeRunner{}
		_, err := (capabilities.DBExport{}).Execute(dumpCtx(fr, fsys.NewFake()),
			raw(t, map[string]any{"name": "acme_db", "file": file}))
		if !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%s (%q): want validation, got %v", name, file, err)
		}
		if len(fr.Calls) != 0 {
			t.Fatalf("%s: commands ran for invalid input", name)
		}
	}
}

func TestDBExportRejectsBadDatabaseName(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.DBExport{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"name": "acme`db; DROP", "file": "x.sql"}))
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation, got %v", err)
	}
}

func TestDBImportLoadsAStagedDump(t *testing.T) {
	f := fsys.NewFake()
	_ = f.WriteFile("/var/lib/heropanel/dumps/import-01H.sql", []byte("CREATE TABLE t (id INT);"), 0o600)
	fr := &exec.FakeRunner{}

	res, err := (capabilities.DBImport{}).Execute(dumpCtx(fr, f),
		raw(t, map[string]any{"name": "acme_db", "file": "import-01H.sql"}))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Data["imported"] != true {
		t.Fatalf("result = %+v", res.Data)
	}
	load, ok := findCall(fr.Calls, "/usr/bin/mysql")
	if !ok {
		t.Fatalf("mysql did not run; calls=%+v", fr.Calls)
	}
	if !hasToken(load, "--database=acme_db") ||
		!hasToken(load, "--execute=SOURCE /var/lib/heropanel/dumps/import-01H.sql") {
		t.Fatalf("import did not source the staged file: %+v", load.Args)
	}
	// The customer's data must not be left lying around afterwards.
	if _, still := f.Written("/var/lib/heropanel/dumps/import-01H.sql"); still {
		t.Fatal("the staged dump survived the import")
	}
}

func TestDBImportDecompressesGzip(t *testing.T) {
	f := fsys.NewFake()
	_ = f.WriteFile("/var/lib/heropanel/dumps/import-01H.sql.gz", []byte("gzipped"), 0o600)
	fr := &exec.FakeRunner{}

	if _, err := (capabilities.DBImport{}).Execute(dumpCtx(fr, f),
		raw(t, map[string]any{"name": "acme_db", "file": "import-01H.sql.gz"})); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, ok := findCall(fr.Calls, "/usr/bin/gunzip", "-f", "/var/lib/heropanel/dumps/import-01H.sql.gz"); !ok {
		t.Fatalf("gzipped dump was not decompressed; calls=%+v", fr.Calls)
	}
	// SOURCE gets the decompressed path.
	load, _ := findCall(fr.Calls, "/usr/bin/mysql")
	if !hasToken(load, "--execute=SOURCE /var/lib/heropanel/dumps/import-01H.sql") {
		t.Fatalf("import sourced the wrong path: %+v", load.Args)
	}
}

func TestDBImportRejectsAMissingDump(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.DBImport{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"name": "acme_db", "file": "gone.sql"}))
	if !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatal("commands ran for a missing dump")
	}
}

func TestDBImportRejectsPathsInTheFilename(t *testing.T) {
	f := fsys.NewFake()
	fr := &exec.FakeRunner{}
	for _, file := range []string{"../../etc/passwd.sql", "/etc/shadow.sql", "sub/x.sql"} {
		_, err := (capabilities.DBImport{}).Execute(dumpCtx(fr, f),
			raw(t, map[string]any{"name": "acme_db", "file": file}))
		if !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%q: want validation, got %v", file, err)
		}
	}
}

func TestDBRevokeBuildsTheStatement(t *testing.T) {
	fr := &exec.FakeRunner{}
	res, err := (capabilities.DBRevoke{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{
			"database": "acme_db", "username": "acme", "host": "localhost",
			"privileges": []string{"select", "insert"},
		}))
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if res.Data["revoked"] != true {
		t.Fatalf("result = %+v", res.Data)
	}
	sql := string(fr.Calls[0].Stdin)
	if !strings.Contains(sql, "REVOKE SELECT, INSERT ON `acme_db`.* FROM 'acme'@'localhost'") {
		t.Fatalf("unexpected statement: %s", sql)
	}
	// `REVOKE IF EXISTS` is MySQL 8 syntax that MariaDB rejects outright, and
	// MariaDB is what HeroPanel targets.
	if strings.Contains(sql, "IF EXISTS") {
		t.Fatalf("REVOKE IF EXISTS is not valid on MariaDB: %s", sql)
	}
}

// Revoking access that is already gone is the end state the caller asked for.
// Failing there would break exactly the case where access is already removed.
func TestDBRevokeForgivesAnAbsentGrant(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(exec.Command) (exec.Result, error) {
		return exec.Result{
			ExitCode: 1,
			Stderr:   []byte("ERROR 1141 (42000) at line 1: There is no such grant defined for user 'acme' on host 'localhost'"),
		}, nil
	}}
	res, err := (capabilities.DBRevoke{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"database": "acme_db", "username": "acme", "host": "localhost"}))
	if err != nil {
		t.Fatalf("an absent grant should not be an error: %v", err)
	}
	if res.Data["revoked"] != true || res.Data["already_absent"] != true {
		t.Fatalf("result = %+v", res.Data)
	}
}

// Any other failure must still surface.
func TestDBRevokeSurfacesRealErrors(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(exec.Command) (exec.Result, error) {
		return exec.Result{ExitCode: 1, Stderr: []byte("ERROR 1045 (28000): Access denied")}, nil
	}}
	_, err := (capabilities.DBRevoke{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"database": "acme_db", "username": "acme"}))
	if !errx.IsKind(err, errx.KindUpstream) {
		t.Fatalf("want upstream, got %v", err)
	}
}

func TestDBRevokeRejectsUnknownPrivileges(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.DBRevoke{}).Execute(dumpCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{
			"database": "acme_db", "username": "acme",
			"privileges": []string{"ALL; DROP DATABASE mysql"},
		}))
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation, got %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatal("SQL ran for an invalid privilege")
	}
}
