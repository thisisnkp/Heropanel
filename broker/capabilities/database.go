package capabilities

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// mysqlPath is the MariaDB/MySQL client. Running as root, it authenticates over
// the local socket (unix_socket auth), so no DB password is needed.
const mysqlPath = "/usr/bin/mysql"

// allowedPrivileges is the set of grantable privilege tokens.
var allowedPrivileges = map[string]bool{
	"ALL": true, "SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"CREATE": true, "DROP": true, "ALTER": true, "INDEX": true, "REFERENCES": true,
	"CREATE TEMPORARY TABLES": true, "LOCK TABLES": true, "EXECUTE": true,
	"CREATE VIEW": true, "SHOW VIEW": true, "CREATE ROUTINE": true, "ALTER ROUTINE": true,
	"EVENT": true, "TRIGGER": true,
}

// runSQLResult pipes sql to the mysql client via stdin (never argv, so
// credentials do not appear in the process list) and returns the raw result, so
// a caller can inspect the client's own error text.
func runSQLResult(c capability.Context, sql string) (exec.Result, error) {
	return c.Runner.Run(c.Ctx, exec.Command{
		Path:    mysqlPath,
		Args:    []string{"--protocol=socket"},
		Stdin:   []byte(sql),
		Timeout: 30 * time.Second,
	})
}

// runSQL executes sql and turns any failure into a structured error.
func runSQL(c capability.Context, sql string) error {
	res, err := runSQLResult(c, sql)
	if err != nil {
		return errx.Upstream(err, "mysql_failed", "The database operation failed.")
	}
	if res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "mysql_failed", "The database command returned an error.")
	}
	return nil
}

// escapeSQLString escapes a string for a single-quoted MySQL literal.
func escapeSQLString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\x00", `\0`, "\n", `\n`, "\r", `\r`, `"`, `\"`)
	return r.Replace(s)
}

// ── db.create ────────────────────────────────────────────────────────────────

type DBCreate struct{}

func (DBCreate) Name() string { return "db.create" }

func (DBCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.create.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}
	sql := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", in.Name)
	if err := runSQL(c, sql); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{"name": in.Name, "created": true}}, nil
}

// ── db.drop ──────────────────────────────────────────────────────────────────

type DBDrop struct{}

func (DBDrop) Name() string { return "db.drop" }

func (DBDrop) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.drop.")
	}
	if err := capability.ValidateDBIdentifier(in.Name); err != nil {
		return capability.Result{}, err
	}
	if err := runSQL(c, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", in.Name)); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{"name": in.Name, "dropped": true}}, nil
}

// ── db.user.create ───────────────────────────────────────────────────────────

type DBUserCreate struct{}

func (DBUserCreate) Name() string { return "db.user.create" }

func (DBUserCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Username string `json:"username"`
		Host     string `json:"host"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.user.create.")
	}
	if err := capability.ValidateDBIdentifier(in.Username); err != nil {
		return capability.Result{}, err
	}
	if in.Host == "" {
		in.Host = "localhost"
	}
	if err := capability.ValidateDBHost(in.Host); err != nil {
		return capability.Result{}, err
	}
	if len(in.Password) < 8 {
		return capability.Result{}, errx.Validation("weak_password", "Database password must be at least 8 characters.")
	}

	pw := escapeSQLString(in.Password)
	// Create if absent, then set/rotate the password.
	sql := fmt.Sprintf(
		"CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'; ALTER USER '%s'@'%s' IDENTIFIED BY '%s'; FLUSH PRIVILEGES;",
		in.Username, in.Host, pw, in.Username, in.Host, pw)
	if err := runSQL(c, sql); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{"username": in.Username, "host": in.Host, "created": true}}, nil
}

// ── db.user.drop ─────────────────────────────────────────────────────────────

type DBUserDrop struct{}

func (DBUserDrop) Name() string { return "db.user.drop" }

func (DBUserDrop) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Username string `json:"username"`
		Host     string `json:"host"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.user.drop.")
	}
	if err := capability.ValidateDBIdentifier(in.Username); err != nil {
		return capability.Result{}, err
	}
	if in.Host == "" {
		in.Host = "localhost"
	}
	if err := capability.ValidateDBHost(in.Host); err != nil {
		return capability.Result{}, err
	}
	if err := runSQL(c, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'; FLUSH PRIVILEGES;", in.Username, in.Host)); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{"username": in.Username, "host": in.Host, "dropped": true}}, nil
}

// ── db.grant ─────────────────────────────────────────────────────────────────

type DBGrant struct{}

func (DBGrant) Name() string { return "db.grant" }

func (DBGrant) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Database   string   `json:"database"`
		Username   string   `json:"username"`
		Host       string   `json:"host"`
		Privileges []string `json:"privileges"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.grant.")
	}
	if err := capability.ValidateDBIdentifier(in.Database); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateDBIdentifier(in.Username); err != nil {
		return capability.Result{}, err
	}
	if in.Host == "" {
		in.Host = "localhost"
	}
	if err := capability.ValidateDBHost(in.Host); err != nil {
		return capability.Result{}, err
	}

	privs, err := normalizePrivileges(in.Privileges)
	if err != nil {
		return capability.Result{}, err
	}

	sql := fmt.Sprintf("GRANT %s ON `%s`.* TO '%s'@'%s'; FLUSH PRIVILEGES;",
		strings.Join(privs, ", "), in.Database, in.Username, in.Host)
	if err := runSQL(c, sql); err != nil {
		return capability.Result{}, err
	}
	return capability.Result{Data: map[string]any{"granted": true}}, nil
}

// ── db.revoke ────────────────────────────────────────────────────────────────

// DBRevoke removes a user's privileges on one database. Without it a panel can
// only ever widen access, which makes "grant" a one-way door.
type DBRevoke struct{}

func (DBRevoke) Name() string { return "db.revoke" }

func (DBRevoke) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Database   string   `json:"database"`
		Username   string   `json:"username"`
		Host       string   `json:"host"`
		Privileges []string `json:"privileges"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for db.revoke.")
	}
	if err := capability.ValidateDBIdentifier(in.Database); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateDBIdentifier(in.Username); err != nil {
		return capability.Result{}, err
	}
	if in.Host == "" {
		in.Host = "localhost"
	}
	if err := capability.ValidateDBHost(in.Host); err != nil {
		return capability.Result{}, err
	}
	privs, err := normalizePrivileges(in.Privileges)
	if err != nil {
		return capability.Result{}, err
	}

	// No `REVOKE IF EXISTS` here: that is MySQL 8.0.16+ syntax and MariaDB — the
	// engine HeroPanel actually targets — rejects it outright. Instead run the
	// plain statement and forgive the one error that means "already true".
	sql := fmt.Sprintf("REVOKE %s ON `%s`.* FROM '%s'@'%s'; FLUSH PRIVILEGES;",
		strings.Join(privs, ", "), in.Database, in.Username, in.Host)
	res, err := runSQLResult(c, sql)
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "mysql_failed", "The database operation failed.")
	}
	if res.ExitCode != 0 {
		if isNoSuchGrant(res) {
			// The caller asked for an end state that already holds. Reporting an
			// error here would make "revoke" fail precisely when access is
			// already gone, which is the opposite of useful.
			return capability.Result{Data: map[string]any{"revoked": true, "already_absent": true}}, nil
		}
		return capability.Result{}, errx.New(errx.KindUpstream, "mysql_failed",
			"The revoke failed: "+logTail(res, 300))
	}
	return capability.Result{Data: map[string]any{"revoked": true}}, nil
}

// isNoSuchGrant reports MariaDB's ER_NONEXISTING_GRANT (1141) — "there is no
// such grant defined for user ... on host ...".
func isNoSuchGrant(res exec.Result) bool {
	out := strings.ToLower(string(res.Stderr) + string(res.Stdout))
	return strings.Contains(out, "1141") || strings.Contains(out, "there is no such grant")
}

// normalizePrivileges upper-cases and allowlists privilege tokens, defaulting to
// ALL. Shared by grant and revoke so the two can never drift apart.
func normalizePrivileges(in []string) ([]string, error) {
	privs := in
	if len(privs) == 0 {
		privs = []string{"ALL"}
	}
	out := make([]string, len(privs))
	for i, p := range privs {
		up := strings.ToUpper(strings.TrimSpace(p))
		if !allowedPrivileges[up] {
			return nil, errx.Validation("invalid_privilege", "Unsupported privilege: "+p)
		}
		out[i] = up
	}
	return out, nil
}
