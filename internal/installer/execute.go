package installer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// This file implements the installer's execute phase: it walks the plan, runs
// each step, and journals the outcome to disk after every step so a crashed or
// interrupted run can --resume, and a --rollback can walk the completed steps in
// reverse and run each one's inverse. The design goal is that every mutation is
// either reversible or explicitly recorded as not-reversible, so a failed
// install never leaves a half-configured host with no way back.
//
// Portable filesystem work (mkdir, write, copy, remove) is done directly with
// the os package so it is unit-testable against a temp layout. Privileged,
// non-portable work (package installs, useradd, systemctl, mysql, firewall) goes
// through the Runner interface so tests substitute a recorder and the real run
// shells out.

// StatusReverted marks a step whose inverse has been applied during rollback.
const StatusReverted StepStatus = "reverted"

// Runner executes external commands. env entries are KEY=VALUE strings appended
// to the process environment.
type Runner interface {
	Run(ctx context.Context, env []string, name string, args ...string) error
	Output(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
}

// execRunner is the real Runner: it shells out and streams output to a writer.
type execRunner struct {
	out io.Writer
}

// NewExecRunner returns a Runner that runs commands for real, echoing their
// combined output to w (use os.Stderr for an interactive install).
func NewExecRunner(w io.Writer) Runner {
	if w == nil {
		w = io.Discard
	}
	return execRunner{out: w}
}

func (e execRunner) Run(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = e.out, e.out
	return cmd.Run()
}

func (e execRunner) Output(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	return cmd.Output()
}

// Layout is the set of filesystem locations the installer writes to. It is a
// field of Executor so tests can redirect every path into a temp dir.
type Layout struct {
	Prefix     string // /opt/heropanel
	BinDir     string // /opt/heropanel/bin
	ConfigDir  string // /etc/heropanel
	DataDir    string // /var/lib/heropanel
	RunDir     string // /run/heropanel
	LogDir     string // /var/log/heropanel
	SystemdDir string // /etc/systemd/system
	SourceDir  string // where freshly-built hpd/hp-broker are staged
	Journal    string // path to the install journal
}

// DefaultLayout returns the production filesystem layout. SourceDir defaults to
// the directory of the running installer, so binaries staged next to it are
// found without a flag.
func DefaultLayout() Layout {
	src := ""
	if exe, err := os.Executable(); err == nil {
		src = filepath.Dir(exe)
	}
	return Layout{
		Prefix:     "/opt/heropanel",
		BinDir:     "/opt/heropanel/bin",
		ConfigDir:  "/etc/heropanel",
		DataDir:    "/var/lib/heropanel",
		RunDir:     "/run/heropanel",
		LogDir:     "/var/log/heropanel",
		SystemdDir: "/etc/systemd/system",
		SourceDir:  src,
		Journal:    "/var/lib/heropanel/install-journal.json",
	}
}

const (
	svcUser   = "heropanel"
	svcGroup  = "heropanel"
	brokerSvc = "hp-broker.service"
	hpdSvc    = "hpd.service"
	secretsFn = "secrets.env"
	configFn  = "config.yaml"
)

// Executor runs an install plan and maintains its journal.
type Executor struct {
	Version        string
	Profile        Profile
	Options        Options
	Layout         Layout
	Runner         Runner
	Log            *slog.Logger
	ServiceManager string // "systemd" | "none" — how services are started

	pkgRefreshed bool
	secrets      map[string]string
	// probe checks panel health during verify; nil uses the real HTTP probe.
	// Overridable so tests exercise the systemd start path without a live panel.
	probe func(ctx context.Context, url string) error
}

// NewExecutor wires an executor with sensible defaults. Runner and Log fall back
// to a real runner on stderr and the default logger; ServiceManager is detected.
func NewExecutor(version string, p Profile, o Options, l Layout) *Executor {
	if o.DB == "" {
		o.DB = "mariadb"
	}
	if o.Minimal {
		o.DB = "sqlite"
	}
	if o.Port == 0 {
		o.Port = 8443
	}
	return &Executor{
		Version:        version,
		Profile:        p,
		Options:        o,
		Layout:         l,
		Runner:         NewExecRunner(os.Stderr),
		Log:            slog.Default(),
		ServiceManager: detectServiceManager(),
		secrets:        map[string]string{},
	}
}

// detectServiceManager reports whether systemd can start services here. A
// container without systemd reports "none", in which case the executor writes
// units but does not try to start them.
func detectServiceManager() string {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return "none"
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return "none"
	}
	return "systemd"
}

// Execute runs the plan from the journal, skipping already-completed steps.
// It persists the journal after every step, so an interrupted run resumes
// exactly where it stopped. On a step failure it records the error and stops,
// leaving the journal ready for --resume or --rollback.
func (e *Executor) Execute(ctx context.Context) error {
	e.loadSecrets()
	j, err := e.loadOrInitJournal()
	if err != nil {
		return err
	}
	for i := range j.Steps {
		js := &j.Steps[i]
		if js.Status == StatusDone {
			continue
		}
		e.Log.Info("install step", "id", js.Step.ID, "desc", js.Step.Description)
		act := e.actionFor(js.Step)
		if err := act.apply(ctx); err != nil {
			js.Status = StatusFailed
			js.Error = err.Error()
			_ = e.saveJournal(j)
			return fmt.Errorf("step %q failed: %w", js.Step.ID, err)
		}
		js.Status = StatusDone
		js.Error = ""
		if err := e.saveJournal(j); err != nil {
			return fmt.Errorf("persist journal after %q: %w", js.Step.ID, err)
		}
	}
	e.Log.Info("install complete", "steps", len(j.Steps))
	return nil
}

// Rollback loads the journal and reverses every completed step, most-recent
// first. A revert that fails is logged and rollback continues — the goal is to
// undo as much as possible, not to stop at the first stubborn step.
func (e *Executor) Rollback(ctx context.Context) error {
	e.loadSecrets()
	j, err := e.loadJournal()
	if err != nil {
		return fmt.Errorf("rollback needs a journal: %w", err)
	}
	var failed int
	for i := len(j.Steps) - 1; i >= 0; i-- {
		js := &j.Steps[i]
		if js.Status != StatusDone && js.Status != StatusFailed {
			continue
		}
		act := e.actionFor(js.Step)
		if act.revert == nil {
			e.Log.Info("rollback skip (irreversible)", "id", js.Step.ID)
			js.Status = StatusReverted
			_ = e.saveJournal(j)
			continue
		}
		e.Log.Info("rollback step", "id", js.Step.ID)
		if err := act.revert(ctx); err != nil {
			failed++
			e.Log.Warn("rollback step failed (continuing)", "id", js.Step.ID, "err", err)
		}
		js.Status = StatusReverted
		_ = e.saveJournal(j)
	}
	if failed > 0 {
		return fmt.Errorf("rollback completed with %d step(s) that could not be fully reverted", failed)
	}
	e.Log.Info("rollback complete")
	return nil
}

// action is a step's forward operation and its inverse. A nil revert marks a
// step whose effect the installer will not attempt to undo (e.g. OS packages,
// which other software may now depend on).
type action struct {
	apply  func(ctx context.Context) error
	revert func(ctx context.Context) error
}

// actionFor maps a plan step to its implementation.
func (e *Executor) actionFor(s Step) action {
	switch {
	case s.ID == "user":
		return action{apply: e.applyUser, revert: e.revertUser}
	case s.ID == "dirs":
		return action{apply: e.applyDirs, revert: e.revertDirs}
	case s.ID == "binaries":
		return action{apply: e.applyBinaries, revert: e.revertBinaries}
	case s.ID == "secrets":
		return action{apply: e.applySecrets, revert: e.revertFile(secretsFn)}
	case s.ID == "config":
		return action{apply: e.applyConfig, revert: e.revertFile(configFn)}
	case s.ID == "db.provision":
		return action{apply: e.applyDBProvision, revert: e.revertDBProvision}
	case s.ID == "db.migrate":
		return action{apply: e.applyMigrate} // schema goes away with the DB/data dir
	case s.ID == "services":
		return action{apply: e.applyServices, revert: e.revertServices}
	case s.ID == "webserver.panel":
		return action{apply: e.applyWebserver, revert: e.revertWebserver}
	case s.ID == "firewall":
		return action{apply: e.applyFirewall, revert: e.revertFirewall}
	case s.ID == "verify":
		return action{apply: e.applyVerify}
	case strings.HasPrefix(s.ID, "deps."):
		return action{apply: e.applyDeps(s.ID)}
	case strings.HasPrefix(s.ID, "module."):
		return action{apply: func(ctx context.Context) error {
			e.Log.Warn("module install not implemented; skipping", "module", strings.TrimPrefix(s.ID, "module."))
			return nil
		}}
	default:
		return action{apply: func(context.Context) error { return nil }}
	}
}

// ── packages ─────────────────────────────────────────────────────────────────

func (e *Executor) applyDeps(id string) func(context.Context) error {
	return func(ctx context.Context) error {
		var pkgs []string
		switch id {
		case "deps.base":
			// Deliberately no curl: Rocky 9 ships curl-minimal, and `dnf install
			// curl` conflicts with it. The installer uses net/http, not curl.
			pkgs = []string{"ca-certificates", "tar", "zstd"}
		case "deps.webserver":
			pkgs = []string{"openlitespeed"}
		case "deps.db":
			pkgs = []string{"mariadb"}
		case "deps.redis":
			pkgs = []string{"redis"}
		default:
			return nil
		}
		if !e.pkgRefreshed {
			if err := pkgRefresh(ctx, e.Runner, e.Profile.PkgManager); err != nil {
				return err
			}
			e.pkgRefreshed = true
		}
		return pkgInstall(ctx, e.Runner, e.Profile.PkgManager, pkgs...)
	}
}

// ── user + group ─────────────────────────────────────────────────────────────

func (e *Executor) applyUser(ctx context.Context) error {
	if e.Runner.Run(ctx, nil, "getent", "group", svcGroup) != nil {
		if err := e.Runner.Run(ctx, nil, "groupadd", "--system", svcGroup); err != nil {
			return fmt.Errorf("create group: %w", err)
		}
	}
	if e.Runner.Run(ctx, nil, "id", svcUser) != nil {
		shell := nologinShell()
		if err := e.Runner.Run(ctx, nil, "useradd", "--system", "--gid", svcGroup,
			"--home-dir", e.Layout.Prefix, "--shell", shell, svcUser); err != nil {
			return fmt.Errorf("create user: %w", err)
		}
	}
	return nil
}

func (e *Executor) revertUser(ctx context.Context) error {
	_ = e.Runner.Run(ctx, nil, "userdel", svcUser)
	_ = e.Runner.Run(ctx, nil, "groupdel", svcGroup)
	return nil
}

// nologinShell picks a non-interactive shell that exists on the host, falling
// back across the Debian/RHEL split.
func nologinShell() string {
	for _, p := range []string{"/usr/sbin/nologin", "/sbin/nologin", "/bin/false"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "/bin/false"
}

// ── directories ──────────────────────────────────────────────────────────────

// ownedDirs are created and chowned to the service user; rootDirs are created
// but left root-owned (binaries, config, units).
func (e *Executor) ownedDirs() []string {
	return []string{e.Layout.DataDir, e.Layout.RunDir, e.Layout.LogDir}
}
func (e *Executor) allDirs() []string {
	return append([]string{e.Layout.Prefix, e.Layout.BinDir, e.Layout.ConfigDir}, e.ownedDirs()...)
}

func (e *Executor) applyDirs(ctx context.Context) error {
	for _, d := range e.allDirs() {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	for _, d := range e.ownedDirs() {
		if err := e.Runner.Run(ctx, nil, "chown", "-R", svcUser+":"+svcGroup, d); err != nil {
			return fmt.Errorf("chown %s: %w", d, err)
		}
	}
	return nil
}

func (e *Executor) revertDirs(context.Context) error {
	// Remove only what we created, deepest first, and never the shared parents
	// of ConfigDir/RunDir (/etc, /run) themselves.
	for _, d := range []string{e.Layout.BinDir, e.Layout.Prefix, e.Layout.DataDir, e.Layout.RunDir, e.Layout.LogDir, e.Layout.ConfigDir} {
		_ = os.RemoveAll(d)
	}
	return nil
}

// ── binaries ─────────────────────────────────────────────────────────────────

var installBinaries = []string{"hpd", "hp-broker"}

func (e *Executor) applyBinaries(ctx context.Context) error {
	if err := os.MkdirAll(e.Layout.BinDir, 0o755); err != nil {
		return err
	}
	// Verify every binary against the SHA256SUMS manifest a release ships, before
	// any of it lands in place. A mismatch means the artifact was corrupted or
	// tampered with in transit, and installing it would be the worst possible
	// outcome — so the whole step fails and rolls back.
	sums, err := e.loadChecksums()
	if err != nil {
		return err
	}
	for _, name := range installBinaries {
		src := filepath.Join(e.Layout.SourceDir, name)
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("staged binary %q not found in %s (use --source)", name, e.Layout.SourceDir)
		}
		if sums != nil {
			if err := verifyChecksum(src, sums[name]); err != nil {
				return fmt.Errorf("integrity check failed for %s: %w", name, err)
			}
		}
		dst := filepath.Join(e.Layout.BinDir, name)
		if err := copyFile(src, dst, 0o755); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
	}
	if sums != nil {
		e.Log.Info("binaries verified against SHA256SUMS", "count", len(installBinaries))
	} else {
		e.Log.Warn("no SHA256SUMS manifest in the source dir; installing binaries unverified", "dir", e.Layout.SourceDir)
	}
	return nil
}

// loadChecksums reads the SHA256SUMS manifest (standard `sha256sum` format) from
// the source dir. A missing manifest returns (nil, nil) so a dev flow without one
// still installs — with a warning; a malformed manifest is a hard error.
func (e *Executor) loadChecksums() (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(e.Layout.SourceDir, "SHA256SUMS"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sums := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("malformed SHA256SUMS line: %q", line)
		}
		// sha256sum marks binary-mode entries with a leading '*' on the name.
		sums[filepath.Base(strings.TrimPrefix(f[1], "*"))] = strings.ToLower(f[0])
	}
	return sums, nil
}

// verifyChecksum computes path's SHA-256 and compares it to the expected hex.
func verifyChecksum(path, want string) error {
	if want == "" {
		return fmt.Errorf("no checksum listed in SHA256SUMS for this file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(want) {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, want)
	}
	return nil
}

func (e *Executor) revertBinaries(context.Context) error {
	for _, name := range installBinaries {
		_ = os.Remove(filepath.Join(e.Layout.BinDir, name))
	}
	return nil
}

// ── secrets ──────────────────────────────────────────────────────────────────

func (e *Executor) applySecrets(context.Context) error {
	if err := os.MkdirAll(e.Layout.ConfigDir, 0o755); err != nil {
		return err
	}
	if e.secrets == nil {
		e.secrets = map[string]string{}
	}
	e.secrets["HP_BROKER_TOKEN"] = randHex(32)
	e.secrets["HP_MASTER_KEY"] = randHex(32)
	if e.Options.DB == "mariadb" {
		e.secrets["HP_DB_PASSWORD"] = randHex(18)
	}
	var b strings.Builder
	b.WriteString("# HeroPanel secrets — generated by hp-installer. Do not commit.\n")
	for _, k := range []string{"HP_BROKER_TOKEN", "HP_MASTER_KEY", "HP_DB_PASSWORD"} {
		if v := e.secrets[k]; v != "" {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	path := filepath.Join(e.Layout.ConfigDir, secretsFn)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	// The service user must read the token from the EnvironmentFile.
	_ = e.Runner.Run(context.Background(), nil, "chown", "root:"+svcGroup, path)
	_ = os.Chmod(path, 0o640)
	return nil
}

// loadSecrets rehydrates e.secrets from secrets.env so a --resume or --rollback
// in a fresh process still has the values the config/db steps need.
func (e *Executor) loadSecrets() {
	if e.secrets == nil {
		e.secrets = map[string]string{}
	}
	b, err := os.ReadFile(filepath.Join(e.Layout.ConfigDir, secretsFn))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			e.secrets[k] = v
		}
	}
}

// ── config ───────────────────────────────────────────────────────────────────

func (e *Executor) applyConfig(context.Context) error {
	if err := os.MkdirAll(e.Layout.ConfigDir, 0o755); err != nil {
		return err
	}
	cfg := e.renderConfig()
	path := filepath.Join(e.Layout.ConfigDir, configFn)
	if err := os.WriteFile(path, []byte(cfg), 0o640); err != nil {
		return err
	}
	_ = e.Runner.Run(context.Background(), nil, "chown", "root:"+svcGroup, path)
	return nil
}

func (e *Executor) renderConfig() string {
	dbDriver := e.Options.DB
	var dsn string
	if dbDriver == "sqlite" {
		dsn = filepath.Join(e.Layout.DataDir, "heropanel.db")
	} else {
		dsn = fmt.Sprintf("heropanel:%s@tcp(127.0.0.1:3306)/heropanel?parseTime=true&loc=UTC",
			e.secrets["HP_DB_PASSWORD"])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# HeroPanel configuration — generated by hp-installer %s\n", e.Version)
	fmt.Fprintf(&b, "server:\n  host: 0.0.0.0\n  port: %d\n  tls:\n    enabled: false\n", e.Options.Port)
	fmt.Fprintf(&b, "database:\n  driver: %s\n  dsn: %q\n", dbDriver, dsn)
	// Redis is opt-in: hpd refuses to start if it is configured but unreachable,
	// and the minimal profile has no managed Redis to point at. Omitting the
	// address makes the cache run L1-only (docs/09), which is exactly right for
	// the low-RAM preset.
	if !e.Options.Minimal {
		fmt.Fprintf(&b, "redis:\n  addr: 127.0.0.1:6379\n  db: 0\n")
	}
	fmt.Fprintf(&b, "broker:\n  socket: %s\n  token: %q\n", filepath.Join(e.Layout.RunDir, "broker.sock"), e.secrets["HP_BROKER_TOKEN"])
	fmt.Fprintf(&b, "log:\n  level: info\n  format: json\n")
	fmt.Fprintf(&b, "security:\n  csrf:\n    enabled: false\n")
	return b.String()
}

// ── database ─────────────────────────────────────────────────────────────────

func (e *Executor) applyDBProvision(ctx context.Context) error {
	pw := e.secrets["HP_DB_PASSWORD"]
	stmt := fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS heropanel CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; "+
			"CREATE USER IF NOT EXISTS 'heropanel'@'127.0.0.1' IDENTIFIED BY '%s'; "+
			"GRANT ALL PRIVILEGES ON heropanel.* TO 'heropanel'@'127.0.0.1'; FLUSH PRIVILEGES;", pw)
	return e.Runner.Run(ctx, nil, "mysql", "-e", stmt)
}

func (e *Executor) revertDBProvision(ctx context.Context) error {
	return e.Runner.Run(ctx, nil, "mysql", "-e",
		"DROP DATABASE IF EXISTS heropanel; DROP USER IF EXISTS 'heropanel'@'127.0.0.1'; FLUSH PRIVILEGES;")
}

func (e *Executor) applyMigrate(ctx context.Context) error {
	hpd := filepath.Join(e.Layout.BinDir, "hpd")
	cfg := filepath.Join(e.Layout.ConfigDir, configFn)
	// Run as the service user so a SQLite file is created owned by heropanel,
	// not root — otherwise the daemon could not open it.
	return e.Runner.Run(ctx, nil, "runuser", "-u", svcUser, "--", hpd, "--config", cfg, "--migrate")
}

// ── services (systemd units) ─────────────────────────────────────────────────

func (e *Executor) applyServices(ctx context.Context) error {
	uid := e.serviceUID(ctx)
	units := map[string]string{
		brokerSvc: e.renderBrokerUnit(uid),
		hpdSvc:    e.renderHpdUnit(),
	}
	for name, body := range units {
		if err := os.WriteFile(filepath.Join(e.Layout.SystemdDir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if e.ServiceManager != "systemd" {
		e.Log.Warn("no service manager detected; units written but not started",
			"dir", e.Layout.SystemdDir)
		return nil
	}
	if err := e.Runner.Run(ctx, nil, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	for _, u := range []string{brokerSvc, hpdSvc} {
		if err := e.Runner.Run(ctx, nil, "systemctl", "enable", "--now", u); err != nil {
			return fmt.Errorf("start %s: %w", u, err)
		}
	}
	return nil
}

func (e *Executor) revertServices(ctx context.Context) error {
	if e.ServiceManager == "systemd" {
		for _, u := range []string{hpdSvc, brokerSvc} {
			_ = e.Runner.Run(ctx, nil, "systemctl", "disable", "--now", u)
		}
		_ = e.Runner.Run(ctx, nil, "systemctl", "daemon-reload")
	}
	for _, u := range []string{brokerSvc, hpdSvc} {
		_ = os.Remove(filepath.Join(e.Layout.SystemdDir, u))
	}
	return nil
}

// serviceUID resolves the service user's numeric uid for the broker's
// SO_PEERCRED allowlist. It falls back to -1 (peercred disabled) if the lookup
// fails, so a rendering never blocks the install.
func (e *Executor) serviceUID(ctx context.Context) string {
	out, err := e.Runner.Output(ctx, nil, "id", "-u", svcUser)
	if err != nil {
		return "-1"
	}
	return strings.TrimSpace(string(out))
}

func (e *Executor) renderBrokerUnit(uid string) string {
	return "[Unit]\n" +
		"Description=HeroPanel privileged broker\n" +
		"After=network.target\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"User=root\n" +
		"EnvironmentFile=" + filepath.Join(e.Layout.ConfigDir, secretsFn) + "\n" +
		"Environment=HP_BROKER_ALLOWED_UID=" + uid + "\n" +
		"Environment=HP_BROKER_PANEL_USER=" + svcUser + "\n" +
		"ExecStart=" + filepath.Join(e.Layout.BinDir, "hp-broker") + " --serve --socket " + filepath.Join(e.Layout.RunDir, "broker.sock") + "\n" +
		"Restart=on-failure\n" +
		"RuntimeDirectory=heropanel\n" +
		"NoNewPrivileges=false\n\n" +
		"[Install]\n" +
		"WantedBy=multi-user.target\n"
}

func (e *Executor) renderHpdUnit() string {
	return "[Unit]\n" +
		"Description=HeroPanel control-plane daemon\n" +
		"After=network.target " + brokerSvc + "\n" +
		"Requires=" + brokerSvc + "\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"User=" + svcUser + "\n" +
		"Group=" + svcGroup + "\n" +
		"EnvironmentFile=" + filepath.Join(e.Layout.ConfigDir, secretsFn) + "\n" +
		"ExecStart=" + filepath.Join(e.Layout.BinDir, "hpd") + " --config " + filepath.Join(e.Layout.ConfigDir, configFn) + "\n" +
		"Restart=on-failure\n" +
		"NoNewPrivileges=true\n" +
		"ProtectSystem=strict\n" +
		"ProtectHome=true\n" +
		"PrivateTmp=true\n" +
		"ReadWritePaths=" + e.Layout.DataDir + " " + e.Layout.RunDir + " " + e.Layout.LogDir + "\n\n" +
		"[Install]\n" +
		"WantedBy=multi-user.target\n"
}

// ── webserver (panel vhost) ──────────────────────────────────────────────────

func (e *Executor) applyWebserver(context.Context) error {
	// Minimal marker config: the panel is served by hpd directly in this MVP;
	// the OLS reverse-proxy vhost is written for the operator to include. Kept
	// deliberately small and reversible.
	dir := filepath.Join(e.Layout.ConfigDir, "webserver")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	vhost := fmt.Sprintf("# HeroPanel panel vhost (proxy to hpd on 127.0.0.1:%d)\n"+
		"extProcessor hpd {\n  type proxy\n  address 127.0.0.1:%d\n}\n", e.Options.Port, e.Options.Port)
	return os.WriteFile(filepath.Join(dir, "panel.conf"), []byte(vhost), 0o644)
}

func (e *Executor) revertWebserver(context.Context) error {
	return os.RemoveAll(filepath.Join(e.Layout.ConfigDir, "webserver"))
}

// ── firewall ─────────────────────────────────────────────────────────────────

func (e *Executor) applyFirewall(ctx context.Context) error {
	port := fmt.Sprintf("%d", e.Options.Port)
	switch {
	case hasCmd("ufw"):
		return e.Runner.Run(ctx, nil, "ufw", "allow", port+"/tcp")
	case hasCmd("firewall-cmd"):
		if err := e.Runner.Run(ctx, nil, "firewall-cmd", "--permanent", "--add-port="+port+"/tcp"); err != nil {
			return err
		}
		return e.Runner.Run(ctx, nil, "firewall-cmd", "--reload")
	default:
		e.Log.Warn("no supported firewall (ufw/firewalld) found; skipping port rule", "port", port)
		return nil
	}
}

func (e *Executor) revertFirewall(ctx context.Context) error {
	port := fmt.Sprintf("%d", e.Options.Port)
	switch {
	case hasCmd("ufw"):
		_ = e.Runner.Run(ctx, nil, "ufw", "delete", "allow", port+"/tcp")
	case hasCmd("firewall-cmd"):
		_ = e.Runner.Run(ctx, nil, "firewall-cmd", "--permanent", "--remove-port="+port+"/tcp")
		_ = e.Runner.Run(ctx, nil, "firewall-cmd", "--reload")
	}
	return nil
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ── verify ───────────────────────────────────────────────────────────────────

func (e *Executor) applyVerify(ctx context.Context) error {
	if e.ServiceManager != "systemd" {
		// Without a service manager the installer never started hpd, so there is
		// nothing to probe — the units are written for the operator (or a
		// container harness) to start. Probing here would just burn the timeout.
		e.Log.Warn("verify: no service manager to start the panel; units are written — " +
			"start them to bring the panel up")
		return nil
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", e.Options.Port)
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := e.healthProbe(ctx, url); err == nil {
			e.Log.Info("verify: panel is reachable", "url", url)
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("verify: panel did not become reachable at %s: %v", url, lastErr)
}

// healthProbe performs one health check, using the injected probe when set.
func (e *Executor) healthProbe(ctx context.Context, url string) error {
	if e.probe != nil {
		return e.probe(ctx, url)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

// ── journal persistence ──────────────────────────────────────────────────────

func (e *Executor) loadOrInitJournal() (*Journal, error) {
	if j, err := e.loadJournal(); err == nil {
		e.Log.Info("resuming from existing journal", "path", e.Layout.Journal)
		return j, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(e.Layout.Journal), 0o755); err != nil {
		return nil, err
	}
	j := NewJournal(e.Version, e.Profile, e.Options)
	return &j, e.saveJournal(&j)
}

func (e *Executor) loadJournal() (*Journal, error) {
	b, err := os.ReadFile(e.Layout.Journal)
	if err != nil {
		return nil, err
	}
	var j Journal
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("corrupt journal %s: %w", e.Layout.Journal, err)
	}
	return &j, nil
}

func (e *Executor) saveJournal(j *Journal) error {
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	// Recreate the journal's directory if it is missing: during rollback the
	// dirs step's revert removes DataDir (where the journal lives), and the
	// remaining reverts still need to record their progress. The journal thus
	// survives a rollback as its record.
	if err := os.MkdirAll(filepath.Dir(e.Layout.Journal), 0o755); err != nil {
		return err
	}
	tmp := e.Layout.Journal + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, e.Layout.Journal)
}

// revertFile returns a revert that removes a file under ConfigDir.
func (e *Executor) revertFile(name string) func(context.Context) error {
	return func(context.Context) error {
		return os.Remove(filepath.Join(e.Layout.ConfigDir, name))
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is catastrophic and unrecoverable; a panic here is
		// correct — we must never emit a predictable secret.
		panic("installer: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
