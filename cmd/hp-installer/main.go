// Command hp-installer is HeroPanel's installer core. The public install.sh
// bootstrap fetches and executes it. This MVP implements detection, a
// compatibility verdict, and a dry-run plan (docs/07). The execute/rollback
// phases run privileged package operations and are Linux-only.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/thisisnkp/heropanel/internal/installer"
)

var version = "0.0.0-dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		detect      = flag.Bool("detect", false, "detect the host and print its profile + compatibility")
		plan        = flag.Bool("plan", false, "print the install plan (dry run) and exit")
		jsonOut     = flag.Bool("json", false, "emit JSON")
		channel     = flag.String("channel", "stable", "release channel: stable|beta|nightly")
		dbDriver    = flag.String("db", "mariadb", "control-plane datastore: mariadb|sqlite")
		port        = flag.Int("port", 8443, "panel port")
		minimal     = flag.Bool("minimal", false, "low-RAM preset (SQLite, minimal modules)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("hp-installer", version)
		return
	}

	profile := installer.Detect()
	report := installer.Compatibility(profile)
	opts := installer.Options{Channel: *channel, DB: *dbDriver, Port: *port, Minimal: *minimal}

	switch {
	case *detect:
		emitDetect(*jsonOut, profile, report)
	case *plan:
		emitPlan(*jsonOut, profile, opts)
	default:
		// Full install is not implemented in this MVP; guide the operator.
		emitDetect(false, profile, report)
		fmt.Fprintln(os.Stderr, "\nhp-installer: use --detect or --plan. The execute phase is not part of this MVP.")
		os.Exit(2)
	}

	if report.Verdict == installer.VerdictBlock {
		os.Exit(1)
	}
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
