package capabilities

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// File-integrity monitoring via AIDE. A baseline database of hashes+metadata is
// built for the panel's security-critical paths; a later check reports what was
// added, removed or changed since. Like the ClamAV and Fail2Ban integrations,
// this shells out to a real tool (fixed argv, no shell) and parses its output —
// no bespoke integrity engine. The config is a broker constant (the watched
// paths are policy, not input) and the database lives root-only under
// /var/lib/heropanel/fim.

const (
	aidePath     = "/usr/bin/aide"
	fimDir       = "/var/lib/heropanel/fim"
	fimConfPath  = fimDir + "/aide.conf"
	fimDBPath    = fimDir + "/aide.db"
	fimDBNew     = fimDir + "/aide.db.new"
	fimScopePath = fimDir + "/scope"
)

// aideHeader configures the database locations and the reusable rule. The rule
// tracks permissions, ownership, size, mtime and strong hashes — enough to catch
// a tampered binary or an edited config.
const aideHeader = `# HeroPanel FIM (rendered; do not edit).
database_in=file:` + fimDBPath + `
database_out=file:` + fimDBNew + `
database_new=file:` + fimDBNew + `
gzip_dbout=no
report_url=stdout

HP = p+i+n+u+g+s+b+m+c+sha256
`

// aidePanelRules watch the panel's own security-critical configuration and
// binaries. A missing path is only a warning, so the same config works on any
// host. This is the default ("panel") scope.
const aidePanelRules = `/etc/heropanel HP
/etc/ssh HP
/etc/postfix HP
/etc/dovecot HP
/etc/opendkim.conf HP
/usr/local/bin/hpd HP
/usr/local/bin/hp-broker HP
/usr/sbin/sshd HP
`

// aideHostRules extend the watch to the wider host: all of /etc plus the system
// binary and library trees, which change only through package management and so
// make tampering stand out. The `!` lines exclude paths that legitimately churn
// (mounts, clock skew, machine/dbus identity, the panel's own DB and temp
// files) so the check stays signal, not noise. This is the "host" scope.
const aideHostRules = `/etc HP
/bin HP
/sbin HP
/usr/bin HP
/usr/sbin HP
/lib HP
/lib64 HP
/boot HP
!/etc/mtab$
!/etc/adjtime$
!/etc/resolv.conf$
!/etc/machine-id$
!/etc/hostname$
!/etc/ld.so.cache$
!/var/lib/heropanel
`

// fimScopes is the set of accepted scopes.
var fimScopes = map[string]bool{"panel": true, "host": true}

// renderFIMConf builds the AIDE config for a scope. "host" is a superset of
// "panel" — the wider rules already cover the panel's own /etc paths.
func renderFIMConf(scope string) string {
	if scope == "host" {
		return aideHeader + aideHostRules
	}
	return aideHeader + aidePanelRules
}

// normalizeFIMScope defaults an empty/unknown scope to the safe "panel" set.
func normalizeFIMScope(scope string) string {
	if fimScopes[scope] {
		return scope
	}
	return "panel"
}

// readFIMScope returns the scope recorded at the last init (default "panel"), so
// a check renders the same config the baseline was built from.
func readFIMScope(c capability.Context) string {
	b, err := c.FS.ReadFile(fimScopePath)
	if err != nil {
		return "panel"
	}
	return normalizeFIMScope(strings.TrimSpace(string(b)))
}

func writeFIMConf(c capability.Context, scope string) error {
	if err := c.FS.MkdirAll(fimDir, 0o700); err != nil {
		return errx.Upstream(err, "fim_mkdir_failed", "Could not create the FIM directory.")
	}
	if err := c.FS.WriteFile(fimConfPath, []byte(renderFIMConf(scope)), 0o600); err != nil {
		return errx.Upstream(err, "fim_conf_failed", "Could not write the FIM config.")
	}
	return nil
}

// FIMInit builds (or rebuilds) the baseline database.
type FIMInit struct{}

// Name implements capability.Capability.
func (FIMInit) Name() string { return "fim.init" }

// fimInitArgs selects how wide a baseline to build.
type fimInitArgs struct {
	Scope string `json:"scope"` // panel (default) | host
}

// Execute implements capability.Capability.
func (FIMInit) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var args fimInitArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return capability.Result{}, errx.Validation("fim_bad_args", "The FIM init arguments are malformed.")
		}
	}
	scope := normalizeFIMScope(args.Scope)
	if err := writeFIMConf(c, scope); err != nil {
		return capability.Result{}, err
	}
	// Record the scope so a later check builds the identically-scoped config;
	// a mismatch would report the whole extra tree as spuriously added/removed.
	if err := c.FS.WriteFile(fimScopePath, []byte(scope), 0o600); err != nil {
		return capability.Result{}, errx.Upstream(err, "fim_conf_failed", "Could not record the FIM scope.")
	}
	// `aide --init` writes the new database; AIDE never overwrites the active DB
	// itself, so we move the freshly written one into place as the baseline. A
	// host-wide scan hashes the whole binary/library tree, so it needs longer.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: aidePath, Args: []string{"--config", fimConfPath, "--init"}, Timeout: fimTimeout(scope),
	})
	// --init exits 0 on success; some builds exit non-zero merely because the DB
	// did not previously exist. Treat a written new-DB as success.
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "fim_init_failed", "Could not initialise the FIM baseline.")
	}
	if ok, _ := c.FS.Exists(fimDBNew); !ok {
		return capability.Result{}, errx.New(errx.KindUpstream, "fim_init_failed",
			"AIDE did not write a baseline database.")
	}
	if mv, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: mvPath, Args: []string{"-f", fimDBNew, fimDBPath}, Timeout: 30 * time.Second,
	}); err != nil || mv.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "fim_init_failed",
			"Could not activate the FIM baseline database.")
	}
	_ = res
	return capability.Result{Data: map[string]any{"initialised": true, "scope": scope}}, nil
}

// fimTimeout scales the AIDE run budget with the scope: a host-wide baseline
// hashes far more of the filesystem than the panel set.
func fimTimeout(scope string) time.Duration {
	if scope == "host" {
		return 30 * time.Minute
	}
	return 5 * time.Minute
}

// FIMCheck compares the filesystem against the baseline.
type FIMCheck struct{}

// Name implements capability.Capability.
func (FIMCheck) Name() string { return "fim.check" }

// Execute implements capability.Capability.
func (FIMCheck) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	if ok, _ := c.FS.Exists(fimDBPath); !ok {
		return capability.Result{}, errx.New(errx.KindConflict, "fim_no_baseline",
			"No FIM baseline exists yet; initialise it first.")
	}
	// Render the config for the scope the baseline was built with, so the check
	// compares like with like rather than reporting the wider tree as changed.
	scope := readFIMScope(c)
	if err := writeFIMConf(c, scope); err != nil {
		return capability.Result{}, err
	}
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: aidePath, Args: []string{"--config", fimConfPath, "--check"}, Timeout: fimTimeout(scope),
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "fim_check_failed", "The FIM check could not run.")
	}
	// AIDE exit codes: 0 = no changes; 1|2|4 (bitmask) = added/removed/changed;
	// >=14 = an operational error.
	out := string(res.Stdout)
	if res.ExitCode >= 14 {
		return capability.Result{}, errx.New(errx.KindUpstream, "fim_check_failed",
			"AIDE reported an error running the check.")
	}
	added, removed, changed := parseAIDESummary(out)
	dirty := res.ExitCode != 0 || added+removed+changed > 0
	return capability.Result{Data: map[string]any{
		"changed":       dirty,
		"added":         added,
		"removed":       removed,
		"changed_count": changed,
		"scope":         scope,
		"report":        clampReport(out),
	}}, nil
}

// FIMStatus reports whether a baseline exists.
type FIMStatus struct{}

// Name implements capability.Capability.
func (FIMStatus) Name() string { return "fim.status" }

// Execute implements capability.Capability.
func (FIMStatus) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	present, _ := c.FS.Exists(fimDBPath)
	tool, _ := c.FS.Exists(aidePath)
	scope := ""
	if present {
		scope = readFIMScope(c)
	}
	return capability.Result{Data: map[string]any{"baseline": present, "tool_present": tool, "scope": scope}}, nil
}

// parseAIDESummary pulls the added/removed/changed counts from AIDE's summary.
// AIDE prints the same labels twice — once in the "Summary" block with a count,
// and again as a bare header over the detail section — so a count is only taken
// when the line actually ends in a number (the bare headers are ignored).
func parseAIDESummary(out string) (added, removed, changed int) {
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(l, "Added entries:"):
			if n, ok := lastInt(l); ok {
				added = n
			}
		case strings.HasPrefix(l, "Removed entries:"):
			if n, ok := lastInt(l); ok {
				removed = n
			}
		case strings.HasPrefix(l, "Changed entries:"):
			if n, ok := lastInt(l); ok {
				changed = n
			}
		}
	}
	return
}

func lastInt(s string) (int, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(fields[len(fields)-1])
	return n, err == nil
}

// clampReport bounds the report so a huge diff cannot bloat the response.
func clampReport(s string) string {
	const max = 8 << 10
	if len(s) > max {
		return s[:max] + "\n… (truncated)"
	}
	return s
}
