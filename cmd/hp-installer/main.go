// Command hp-installer is HeroPanel's installer core. The public install.sh
// bootstrap fetches and executes it. It detects the host, renders a
// compatibility verdict, prints a dry-run plan, and — with --execute — runs the
// privileged install, journaling each step so an interrupted run can --resume
// and a failed one can --rollback (docs/07). The execute/rollback phases run
// privileged package operations and are Linux-only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/thisisnkp/heropanel/internal/installer"
	"github.com/thisisnkp/heropanel/pkg/logx"
)

var version = "0.0.0-dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		detect      = flag.Bool("detect", false, "detect the host and print its profile + compatibility")
		plan        = flag.Bool("plan", false, "print the install plan (dry run) and exit")
		execute     = flag.Bool("execute", false, "run the install (privileged, Linux-only)")
		resume      = flag.Bool("resume", false, "resume an interrupted install from its journal")
		rollback    = flag.Bool("rollback", false, "reverse a completed/failed install from its journal")
		jsonOut     = flag.Bool("json", false, "emit JSON")
		channel     = flag.String("channel", "stable", "release channel: stable|beta|nightly")
		dbDriver    = flag.String("db", "mariadb", "control-plane datastore: mariadb|sqlite")
		port        = flag.Int("port", 8443, "panel port")
		minimal     = flag.Bool("minimal", false, "low-RAM preset (SQLite, minimal modules)")
		noWebServer = flag.Bool("no-webserver", false, "do not install/configure the site web server")
		source      = flag.String("source", "", "directory holding the staged hpd/hp-broker binaries")
		yes         = flag.Bool("yes", false, "proceed without the interactive confirmation")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("hp-installer", version)
		return
	}

	profile := installer.Detect()
	report := installer.Compatibility(profile)
	opts := installer.Options{Channel: *channel, DB: *dbDriver, Port: *port, Minimal: *minimal, NoWebServer: *noWebServer}

	switch {
	case *detect:
		emitDetect(*jsonOut, profile, report)
	case *plan:
		emitPlan(*jsonOut, profile, opts)
	case *execute || *resume:
		os.Exit(runExecute(profile, report, opts, *source, *yes))
	case *rollback:
		os.Exit(runRollback(profile, opts, *source))
	default:
		emitDetect(false, profile, report)
		fmt.Fprintln(os.Stderr, "\nhp-installer: use --detect, --plan, --execute, --resume, or --rollback.")
		os.Exit(2)
	}

	if report.Verdict == installer.VerdictBlock {
		os.Exit(1)
	}
}

// newExecutor builds the executor with a real runner and a text logger, honoring
// a --source override for the staged binaries.
func newExecutor(profile installer.Profile, opts installer.Options, source string) *installer.Executor {
	layout := installer.DefaultLayout()
	if source != "" {
		layout.SourceDir = source
	}
	log := logx.New(os.Stderr, logx.Options{Level: slog.LevelInfo, Format: logx.FormatText})
	ex := installer.NewExecutor(version, profile, opts, layout)
	ex.Log = log
	ex.Runner = installer.NewExecRunner(os.Stderr)
	return ex
}

func runExecute(profile installer.Profile, report installer.Report, opts installer.Options, source string, yes bool) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "hp-installer: --execute is Linux-only")
		return 2
	}
	if report.Verdict == installer.VerdictBlock {
		emitDetect(false, profile, report)
		fmt.Fprintln(os.Stderr, "\nhp-installer: host is not compatible; refusing to install.")
		return 1
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "hp-installer: --execute must run as root")
		return 1
	}
	if !yes {
		emitPlan(false, profile, opts)
		fmt.Fprint(os.Stderr, "\nProceed with the install? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" && answer != "yes" {
			fmt.Fprintln(os.Stderr, "aborted.")
			return 130
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ex := newExecutor(profile, opts, source)
	if err := ex.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "\nhp-installer:", err)
		fmt.Fprintln(os.Stderr, "The journal is preserved. Re-run with --resume after fixing the cause, or --rollback to undo.")
		return 1
	}
	fmt.Fprintln(os.Stderr, "\nHeroPanel installed. The panel is listening on port", opts.Port)
	return 0
}

func runRollback(profile installer.Profile, opts installer.Options, source string) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "hp-installer: --rollback is Linux-only")
		return 2
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "hp-installer: --rollback must run as root")
		return 1
	}
	ctx := context.Background()
	ex := newExecutor(profile, opts, source)
	if err := ex.Rollback(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "hp-installer:", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "Rollback complete.")
	return 0
}

func emitDetect(asJSON bool, p installer.Profile, r installer.Report) {
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"profile": p, "compatibility": r})
		return
	}
	fmt.Println("HeroPanel — system profile")
	fmt.Printf("  OS/arch     : %s/%s\n", p.OS, p.Arch)
	fmt.Printf("  Distribution: %s (%s %s)\n", or(p.DistroName, "unknown"), p.DistroID, p.DistroVersion)
	fmt.Printf("  Pkg manager : %s\n", or(p.PkgManager, "none"))
	fmt.Printf("  CPU / RAM   : %d cores / %s\n", p.CPUCores, humanBytes(p.RAMBytes))
	fmt.Printf("  systemd     : %v\n", p.HasSystemd)
	fmt.Printf("  virtualized : %s\n", p.Virtualization)
	fmt.Printf("\nCompatibility: %s\n", r.Verdict)
	for _, b := range r.Blocks {
		fmt.Printf("  ✗ %s\n", b)
	}
	for _, wn := range r.Warnings {
		fmt.Printf("  ! %s\n", wn)
	}
}

func emitPlan(asJSON bool, p installer.Profile, o installer.Options) {
	steps := installer.Plan(p, o)
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"options": o, "steps": steps})
		return
	}
	fmt.Printf("Install plan (%s channel, db=%s, port=%d):\n", o.Channel, planDB(o), o.Port)
	for i, s := range steps {
		fmt.Printf("  %2d. [%-9s] %s\n", i+1, s.Kind, s.Description)
	}
}

func planDB(o installer.Options) string {
	if o.Minimal {
		return "sqlite"
	}
	if o.DB == "" {
		return "mariadb"
	}
	return o.DB
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func humanBytes(b uint64) string {
	if b == 0 {
		return "unknown"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
