package capabilities

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// dumpRoot is where database exports and staged imports live. hpd streams files
// to and from here, so the directory is owned by the panel user; the SQL itself
// still only ever runs as root through this broker.
const dumpRoot = "/var/lib/heropanel/dumps"

const (
	mysqldumpPath = "/usr/bin/mysqldump"
	gzipPath      = "/usr/bin/gzip"
	gunzipPath    = "/usr/bin/gunzip"
	statPath      = "/usr/bin/stat"
	chmodPath     = "/bin/chmod"
	chownPath     = "/bin/chown"
)

// reDumpFile bounds the dump filename. It is a bare name — no directories, no
// dots that could climb — so the path built from it cannot leave dumpRoot. This
// mirrors how cert.go derives a path from a validated FQDN.
var reDumpFile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}\.sql(\.gz)?$`)

func validateDumpFile(name string) error {
	if !reDumpFile.MatchString(name) || strings.Contains(name, "..") {
		return errx.Validation("invalid_dump_file", "Invalid dump filename.")
	}
	return nil
}

// runSQLOut is runSQL's read variant: it returns the client's stdout so a
// capability can report a value (a size, a count) rather than just success.
func runSQLOut(c capability.Context, sql string) (string, error) {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    mysqlPath,
		Args:    []string{"--protocol=socket", "--batch", "--skip-column-names"},
		Stdin:   []byte(sql),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return "", errx.Upstream(err, "mysql_failed", "The database query failed.")
	}
	if res.ExitCode != 0 {
		return "", errx.New(errx.KindUpstream, "mysql_failed", "The database query returned an error.")
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// ── db.size ──────────────────────────────────────────────────────────────────

// DBSize reports a database's on-disk size. Approximate by nature:
// information_schema's numbers come from the engine's own statistics and can lag
// reality, which is fine for a panel's "how big is this?" display and would be
// wrong to present as an exact figure.
type DBSize struct{}

func (DBSize) Name() string { return "db.size" }

func (DBSize) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.size.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}

	// COALESCE so a database with no tables reports 0 rather than a NULL that
	// would parse as an error.
	sql := "SELECT COALESCE(SUM(data_length + index_length), 0), COUNT(*) " +
		"FROM information_schema.TABLES WHERE table_schema = '" + escapeSQLString(in.Name) + "';"
	out, err := runSQLOut(c, sql)
	if err != nil {
		return capability.Result{}, err
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return capability.Result{}, errx.New(errx.KindUpstream, "mysql_failed", "Could not read the database size.")
	}
	bytesN, _ := strconv.ParseInt(fields[0], 10, 64)
	tables, _ := strconv.ParseInt(fields[1], 10, 64)
	return capability.Result{Data: map[string]any{
		"name": in.Name, "bytes": bytesN, "tables": tables,
	}}, nil
}

// ── db.export ────────────────────────────────────────────────────────────────

// DBExport dumps a database to a gzipped file under dumpRoot, owned by the panel
// user so hpd can stream it to the client.
//
// The dump goes straight to a file via --result-file and is compressed as a
// separate step. Neither the dump nor the gzip ever passes through the broker's
// memory or its JSON transport: a multi-gigabyte database would otherwise have
// to be buffered whole and base64'd over a socket.
type DBExport struct{}

func (DBExport) Name() string { return "db.export" }

func (DBExport) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
		File string `json:"file"` // bare filename, ".sql"
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.export.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}
	if err := validateDumpFile(in.File); err != nil {
		return capability.Result{}, err
	}
	if strings.HasSuffix(in.File, ".gz") {
		return capability.Result{}, errx.Validation("invalid_dump_file",
			"Pass the uncompressed name; the export is gzipped for you.")
	}

	// The directory belongs to the panel user, not root: hpd has to traverse it to
	// stream the dump back out, and 0700 root would lock it out of its own export.
	if err := ensureDumpRoot(c); err != nil {
		return capability.Result{}, err
	}
	sqlFile := dumpRoot + "/" + in.File
	gzFile := sqlFile + ".gz"

	// Remove any leftover from an interrupted run: gzip refuses to clobber, and a
	// stale .sql would otherwise be re-compressed and served as this export.
	_ = c.FS.Remove(sqlFile)
	_ = c.FS.Remove(gzFile)

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: mysqldumpPath,
		Args: []string{
			"--protocol=socket",
			"--single-transaction", // consistent snapshot without locking the site out
			"--quick",              // stream rows instead of buffering the table
			"--routines", "--triggers", "--events",
			"--default-character-set=utf8mb4",
			"--result-file=" + sqlFile,
			in.Name,
		},
		Timeout: 60 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "export_failed", "Could not run mysqldump.")
	}
	if res.ExitCode != 0 {
		_ = c.FS.Remove(sqlFile)
		return capability.Result{}, errx.New(errx.KindUpstream, "export_failed",
			"mysqldump failed: "+logTail(res, 500))
	}

	gz, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: gzipPath, Args: []string{"-f", sqlFile}, Timeout: 30 * time.Minute,
	})
	if err != nil || gz.ExitCode != 0 {
		_ = c.FS.Remove(sqlFile)
		return capability.Result{}, errx.New(errx.KindUpstream, "export_failed", "Could not compress the dump.")
	}

	// Hand it to the panel user; hpd streams the file and then deletes it.
	if err := handToPanel(c, gzFile); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{
		"name":  in.Name,
		"path":  gzFile,
		"bytes": fileSize(c, gzFile),
	}}, nil
}

// ── db.import ────────────────────────────────────────────────────────────────

// DBImport loads a SQL file (plain or gzipped) that hpd staged under dumpRoot.
//
// The file is fed to the client with `SOURCE`, not piped through the broker, for
// the same reason exports are not: an import is arbitrarily large. The path is
// derived from a validated bare filename, so nothing the caller sends can point
// SOURCE at a file outside dumpRoot.
type DBImport struct{}

func (DBImport) Name() string { return "db.import" }

func (DBImport) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
		File string `json:"file"` // bare filename, ".sql" or ".sql.gz"
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.import.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}
	if err := validateDumpFile(in.File); err != nil {
		return capability.Result{}, err
	}

	path := dumpRoot + "/" + in.File
	if ok, err := c.FS.Exists(path); err != nil || !ok {
		return capability.Result{}, errx.NotFound("dump_not_found", "The uploaded dump could not be found.")
	}

	// Decompress in place first: SOURCE cannot read gzip.
	if strings.HasSuffix(path, ".gz") {
		res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: gunzipPath, Args: []string{"-f", path}, Timeout: 30 * time.Minute,
		})
		if err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "import_failed",
				"Could not decompress the uploaded dump.")
		}
		path = strings.TrimSuffix(path, ".gz")
	}

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: mysqlPath,
		Args: []string{
			"--protocol=socket",
			"--default-character-set=utf8mb4",
			"--database=" + in.Name,
			// Stop at the first error instead of leaving a half-loaded database.
			"--execute=SOURCE " + path,
		},
		Timeout: 60 * time.Minute,
	})
	// The staged file has served its purpose either way; never leave a customer's
	// data lying around on disk.
	_ = c.FS.Remove(path)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "import_failed", "Could not run the import.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "import_failed",
			"The import failed: "+logTail(res, 1000))
	}
	return capability.Result{Data: map[string]any{"name": in.Name, "imported": true}}, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// ensureDumpRoot creates the dump directory owned by the panel user, 0700.
// install(1) does create-and-own in one step and is idempotent, so this is safe
// to call on every export.
func ensureDumpRoot(c capability.Context) error {
	user := c.Policy.EffectivePanelUser()
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    installPath,
		Args:    []string{"-d", "-m", "0700", "-o", user, "-g", user, dumpRoot},
		Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "export_failed", "Could not create the dump directory.")
	}
	return nil
}

// handToPanel gives an existing file to the panel user, readable only by them.
//
// The 0600 is the point, not a detail: mysqldump creates its output under root's
// umask (0644), which would leave one customer's full database dump readable by
// every other site user on the box.
func handToPanel(c capability.Context, path string) error {
	user := c.Policy.EffectivePanelUser()
	steps := []exec.Command{
		{Path: chmodPath, Args: []string{"0600", path}, Timeout: 30 * time.Second},
		{Path: chownPath, Args: []string{user + ":" + user, path}, Timeout: 30 * time.Second},
	}
	for _, cmd := range steps {
		res, err := c.Runner.Run(c.Ctx, cmd)
		if err != nil || res.ExitCode != 0 {
			return errx.New(errx.KindUpstream, "export_failed", "Could not hand the dump to the panel.")
		}
	}
	return nil
}

// fileSize best-effort stats a file; 0 when unknown (display only).
func fileSize(c capability.Context, path string) int64 {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: statPath, Args: []string{"-c", "%s", path}, Timeout: 10 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(res.Stdout)), 10, 64)
	return n
}
