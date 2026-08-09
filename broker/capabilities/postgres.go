package capabilities

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// PostgreSQL support, the second database engine.
//
// Unlike MariaDB (root authenticates over the unix socket), PostgreSQL admin
// runs as the **postgres OS user** via peer auth — so every statement is run
// through `runuser -u postgres -- psql` with SQL on stdin (never argv, so a
// password never reaches the process list). Identifiers are strictly validated
// (ValidateDBIdentifier) and still double-quoted; string literals (passwords) are
// escaped for PostgreSQL's default standard_conforming_strings (only `'` needs
// doubling — backslashes are literal), so neither can break out of its context.
//
// pg_dump cannot write into the panel's 0700 dump dir (it runs as postgres, not
// the panel user), so exports go to a postgres-owned staging dir first and root
// then gzips and moves them into the panel's dump dir; imports go the other way.
const (
	psqlPath    = "/usr/bin/psql"
	pgDumpPath  = "/usr/bin/pg_dump"
	pgUser      = "postgres"
	pgStageRoot = "/var/lib/nexpanel/pgstage"
)

// pgPrivileges is the set of grantable PostgreSQL privilege tokens the panel
// exposes (a practical subset; "ALL" is the common case).
var pgPrivileges = map[string]bool{
	"ALL": true, "SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"TRUNCATE": true, "REFERENCES": true, "TRIGGER": true,
}

// escapePGString escapes a string for a single-quoted PostgreSQL literal
// (standard_conforming_strings on: only the quote itself is special).
func escapePGString(s string) string { return strings.ReplaceAll(s, "'", "''") }

// pgExec runs SQL as the postgres user against db ("" => the default database),
// returning the raw result so callers can inspect the client's error text.
func pgExec(c capability.Context, db, sql string) (exec.Result, error) {
	args := []string{"-v", "ON_ERROR_STOP=1", "-q"}
	if db != "" {
		args = append(args, "-d", db)
	}
	return runAsUserStdin(c, pgUser, "", nil, []byte(sql), 60*time.Second,
		append([]string{psqlPath}, args...)...)
}

// pgQuery runs a scalar query as postgres and returns the trimmed single value
// (-t tuples-only, -A unaligned).
func pgQuery(c capability.Context, db, sql string) (string, error) {
	args := []string{"-t", "-A", "-q"}
	if db != "" {
		args = append(args, "-d", db)
	}
	res, err := runAsUserStdin(c, pgUser, "", nil, []byte(sql), 30*time.Second,
		append([]string{psqlPath}, args...)...)
	if err != nil {
		return "", errx.Upstream(err, "pg_failed", "The PostgreSQL query failed.")
	}
	if res.ExitCode != 0 {
		return "", errx.New(errx.KindUpstream, "pg_failed", "The PostgreSQL query returned an error.")
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// ── pg.create ─────────────────────────────────────────────────────────────────

type PGCreate struct{}

func (PGCreate) Name() string { return "pg.create" }

func (PGCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for pg.create.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}
	// PostgreSQL has no CREATE DATABASE IF NOT EXISTS, so forgive the one error
	// that means "already there" (SQLSTATE 42P04 / duplicate_database).
	res, err := pgExec(c, "", fmt.Sprintf(`CREATE DATABASE "%s";`, in.Name))
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "pg_failed", "The database operation failed.")
	}
	if res.ExitCode != 0 && !pgAlreadyExists(res) {
		return capability.Result{}, errx.New(errx.KindUpstream, "pg_failed",
			"Could not create the database: "+logTail(res, 300))
	}
	return capability.Result{Data: map[string]any{"name": in.Name, "created": true}}, nil
}

// ── pg.drop ───────────────────────────────────────────────────────────────────

type PGDrop struct{}

func (PGDrop) Name() string { return "pg.drop" }

func (PGDrop) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for pg.drop.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}
	res, err := pgExec(c, "", fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, in.Name))
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "pg_failed", "The database operation failed.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "pg_failed",
			"Could not drop the database: "+logTail(res, 300))
	}
	return capability.Result{Data: map[string]any{"name": in.Name, "dropped": true}}, nil
}

// ── pg.user.create ────────────────────────────────────────────────────────────

type PGUserCreate struct{}

func (PGUserCreate) Name() string { return "pg.user.create" }

func (PGUserCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for pg.user.create.")
	}
	if err := capability.ValidateDBIdentifier(in.Username); err != nil {
		return capability.Result{}, err
	}
	if len(in.Password) < 8 {
		return capability.Result{}, errx.Validation("weak_password", "Database password must be at least 8 characters.")
	}
	pw := escapePGString(in.Password)
	name := escapePGString(in.Username)
	// Create-or-update the role atomically in a DO block, so a rotated password
	// applies whether or not the role already exists.
	sql := fmt.Sprintf(`DO $np$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN
    CREATE ROLE "%s" LOGIN PASSWORD '%s';
  ELSE
    ALTER ROLE "%s" LOGIN PASSWORD '%s';
  END IF;
END $np$;`, name, in.Username, pw, in.Username, pw)
	res, err := pgExec(c, "", sql)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "pg_failed", "The database operation failed.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "pg_failed",
			"Could not create the role: "+logTail(res, 300))
	}
	return capability.Result{Data: map[string]any{"username": in.Username, "created": true}}, nil
}

// ── pg.user.drop ──────────────────────────────────────────────────────────────

type PGUserDrop struct{}

func (PGUserDrop) Name() string { return "pg.user.drop" }

func (PGUserDrop) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for pg.user.drop.")
	}
	if err := capability.ValidateDBIdentifier(in.Username); err != nil {
		return capability.Result{}, err
	}
	res, err := pgExec(c, "", fmt.Sprintf(`DROP ROLE IF EXISTS "%s";`, in.Username))
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "pg_failed", "The database operation failed.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "pg_failed",
			"Could not drop the role (it may still own objects): "+logTail(res, 300))
	}
	return capability.Result{Data: map[string]any{"username": in.Username, "dropped": true}}, nil
}

// ── pg.grant ──────────────────────────────────────────────────────────────────

type PGGrant struct{}

func (PGGrant) Name() string { return "pg.grant" }

func (PGGrant) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	in, privs, err := decodePGGrant(raw)
	if err != nil {
		return capability.Result{}, err
	}
	// CONNECT on the database, then the requested privileges on the schema's
	// objects (present and future). "ALL" is expanded to a role that can fully use
	// the database — the common "give this user the database" intent.
	sql := pgGrantSQL(in.Database, in.Username, privs, true)
	res, err := pgExec(c, in.Database, sql)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "pg_failed", "The grant failed.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "pg_failed", "The grant failed: "+logTail(res, 300))
	}
	return capability.Result{Data: map[string]any{"granted": true}}, nil
}

// ── pg.revoke ─────────────────────────────────────────────────────────────────

type PGRevoke struct{}

func (PGRevoke) Name() string { return "pg.revoke" }

func (PGRevoke) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	in, privs, err := decodePGGrant(raw)
	if err != nil {
		return capability.Result{}, err
	}
	sql := pgGrantSQL(in.Database, in.Username, privs, false)
	res, err := pgExec(c, in.Database, sql)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "pg_failed", "The revoke failed.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "pg_failed", "The revoke failed: "+logTail(res, 300))
	}
	return capability.Result{Data: map[string]any{"revoked": true}}, nil
}

type pgGrantInput struct {
	Database   string   `json:"database"`
	Username   string   `json:"username"`
	Privileges []string `json:"privileges"`
}

func decodePGGrant(raw json.RawMessage) (pgGrantInput, []string, error) {
	var in pgGrantInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, nil, errx.Validation("bad_input", "Invalid grant input.")
	}
	if err := capability.ValidateDBIdentifier(in.Database); err != nil {
		return in, nil, err
	}
	if err := capability.ValidateDBIdentifier(in.Username); err != nil {
		return in, nil, err
	}
	privs := in.Privileges
	if len(privs) == 0 {
		privs = []string{"ALL"}
	}
	out := make([]string, len(privs))
	for i, p := range privs {
		up := strings.ToUpper(strings.TrimSpace(p))
		if !pgPrivileges[up] {
			return in, nil, errx.Validation("invalid_privilege", "Unsupported privilege: "+p)
		}
		out[i] = up
	}
	return in, out, nil
}

// pgGrantSQL builds the GRANT (grant=true) or REVOKE statements for a database.
// The statements run *inside* the target database (pgExec with db set), which is
// required for the schema/table grants to affect the right database.
func pgGrantSQL(db, user string, privs []string, grant bool) string {
	q := `"` + user + `"`
	list := strings.Join(privs, ", ")
	var b strings.Builder
	if grant {
		fmt.Fprintf(&b, `GRANT CONNECT ON DATABASE "%s" TO %s;`+"\n", db, q)
		fmt.Fprintf(&b, "GRANT USAGE ON SCHEMA public TO %s;\n", q)
		fmt.Fprintf(&b, "GRANT %s ON ALL TABLES IN SCHEMA public TO %s;\n", list, q)
		fmt.Fprintf(&b, "GRANT %s ON ALL SEQUENCES IN SCHEMA public TO %s;\n", list, q)
		// Future objects too, so the user does not lose access to tables created
		// later — the usual surprise with PostgreSQL grants.
		fmt.Fprintf(&b, "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT %s ON TABLES TO %s;\n", list, q)
		if contains(privs, "ALL") {
			fmt.Fprintf(&b, "GRANT ALL ON SCHEMA public TO %s;\n", q)
		}
	} else {
		fmt.Fprintf(&b, "ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE %s ON TABLES FROM %s;\n", list, q)
		fmt.Fprintf(&b, "REVOKE %s ON ALL SEQUENCES IN SCHEMA public FROM %s;\n", list, q)
		fmt.Fprintf(&b, "REVOKE %s ON ALL TABLES IN SCHEMA public FROM %s;\n", list, q)
		fmt.Fprintf(&b, `REVOKE CONNECT ON DATABASE "%s" FROM %s;`+"\n", db, q)
	}
	return b.String()
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// ── pg.size ───────────────────────────────────────────────────────────────────

type PGSize struct{}

func (PGSize) Name() string { return "pg.size" }

func (PGSize) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for pg.size.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}
	out, err := pgQuery(c, "", fmt.Sprintf("SELECT pg_database_size('%s');", escapePGString(in.Name)))
	if err != nil {
		return capability.Result{}, err
	}
	bytesN, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	tables, _ := pgQuery(c, in.Name,
		"SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema');")
	tablesN, _ := strconv.ParseInt(strings.TrimSpace(tables), 10, 64)
	return capability.Result{Data: map[string]any{"name": in.Name, "bytes": bytesN, "tables": tablesN}}, nil
}

// ── pg.export ─────────────────────────────────────────────────────────────────

type PGExport struct{}

func (PGExport) Name() string { return "pg.export" }

func (PGExport) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
		File string `json:"file"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for pg.export.")
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
	if err := ensureDumpRoot(c); err != nil {
		return capability.Result{}, err
	}
	if err := ensurePGStage(c); err != nil {
		return capability.Result{}, err
	}
	stageFile := pgStageRoot + "/" + in.File // postgres can write here
	_ = c.FS.Remove(stageFile)
	_ = c.FS.Remove(stageFile + ".gz")

	// pg_dump runs as postgres, writing to the postgres-owned staging dir.
	res, err := runAsUser(c, pgUser, "", nil, 60*time.Minute,
		pgDumpPath, "--no-owner", "--no-privileges", "-f", stageFile, in.Name)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "export_failed", "Could not run pg_dump.")
	}
	if res.ExitCode != 0 {
		_ = c.FS.Remove(stageFile)
		return capability.Result{}, errx.New(errx.KindUpstream, "export_failed", "pg_dump failed: "+logTail(res, 500))
	}
	// gzip (root), then move into the panel's dump dir and hand it over.
	if gz, err := c.Runner.Run(c.Ctx, exec.Command{Path: gzipPath, Args: []string{"-f", stageFile}, Timeout: 30 * time.Minute}); err != nil || gz.ExitCode != 0 {
		_ = c.FS.Remove(stageFile)
		return capability.Result{}, errx.New(errx.KindUpstream, "export_failed", "Could not compress the dump.")
	}
	gzStage := stageFile + ".gz"
	gzFinal := dumpRoot + "/" + in.File + ".gz"
	if mv, err := c.Runner.Run(c.Ctx, exec.Command{Path: mvPath, Args: []string{"-f", gzStage, gzFinal}, Timeout: 5 * time.Minute}); err != nil || mv.ExitCode != 0 {
		_ = c.FS.Remove(gzStage)
		return capability.Result{}, errx.New(errx.KindUpstream, "export_failed", "Could not stage the dump.")
	}
	if err := handToPanel(c, gzFinal); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{"name": in.Name, "path": gzFinal, "bytes": fileSize(c, gzFinal)}}, nil
}

// ── pg.import ─────────────────────────────────────────────────────────────────

type PGImport struct{}

func (PGImport) Name() string { return "pg.import" }

func (PGImport) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
		File string `json:"file"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for pg.import.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}
	if err := validateDumpFile(in.File); err != nil {
		return capability.Result{}, err
	}
	staged := dumpRoot + "/" + in.File // npd staged it here (panel-owned)
	if ok, err := c.FS.Exists(staged); err != nil || !ok {
		return capability.Result{}, errx.NotFound("dump_not_found", "The uploaded dump could not be found.")
	}
	if err := ensurePGStage(c); err != nil {
		return capability.Result{}, err
	}
	// Copy into the postgres-readable staging dir and give it to postgres, since
	// psql (as postgres) cannot read the panel's 0700 dump dir.
	work := pgStageRoot + "/" + in.File
	if cp, err := c.Runner.Run(c.Ctx, exec.Command{Path: cpPath, Args: []string{"-f", staged, work}, Timeout: 5 * time.Minute}); err != nil || cp.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "import_failed", "Could not stage the dump.")
	}
	_, _ = c.Runner.Run(c.Ctx, exec.Command{Path: chownPath, Args: []string{pgUser + ":" + pgUser, work}, Timeout: 30 * time.Second})
	cleanup := func() {
		_ = c.FS.Remove(staged)
		_ = c.FS.Remove(work)
		_ = c.FS.Remove(strings.TrimSuffix(work, ".gz"))
	}

	if strings.HasSuffix(work, ".gz") {
		if g, err := c.Runner.Run(c.Ctx, exec.Command{Path: gunzipPath, Args: []string{"-f", work}, Timeout: 30 * time.Minute}); err != nil || g.ExitCode != 0 {
			cleanup()
			return capability.Result{}, errx.New(errx.KindUpstream, "import_failed", "Could not decompress the uploaded dump.")
		}
		work = strings.TrimSuffix(work, ".gz")
	}
	res, err := runAsUser(c, pgUser, "", nil, 60*time.Minute,
		psqlPath, "-v", "ON_ERROR_STOP=1", "-q", "-d", in.Name, "-f", work)
	cleanup()
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "import_failed", "Could not run the import.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "import_failed", "The import failed: "+logTail(res, 1000))
	}
	return capability.Result{Data: map[string]any{"name": in.Name, "imported": true}}, nil
}

// ensurePGStage creates the postgres-owned staging dir for dumps/imports.
func ensurePGStage(c capability.Context) error {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    installPath,
		Args:    []string{"-d", "-m", "0700", "-o", pgUser, "-g", pgUser, pgStageRoot},
		Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "export_failed", "Could not create the PostgreSQL staging directory.")
	}
	return nil
}

// pgAlreadyExists reports PostgreSQL's duplicate_database error (42P04).
func pgAlreadyExists(res exec.Result) bool {
	out := strings.ToLower(string(res.Stderr) + string(res.Stdout))
	return strings.Contains(out, "already exists") || strings.Contains(out, "42p04")
}
