// Command npd is the NexPanel core control-plane daemon: the unprivileged,
// network-facing process that serves the API and (later) the SPA, orchestrates
// work, and talks to the broker and modules. See docs/01 and docs/08.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/thisisnkp/nexpanel/internal/bootstrap"
	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/pkg/logx"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	// `npd decrypt <in> <out>` is the disaster-recovery path: it opens any
	// sealed backup object (site archive, database dump, panel snapshot) with
	// the key derived from NP_SECRET_KEY. It is a subcommand, not a flag, so it
	// works on a box where nothing else does — no config, no database, no
	// broker; just the binary and the master key the operator kept safe.
	if len(os.Args) > 1 && os.Args[1] == "decrypt" {
		os.Exit(runDecrypt(os.Args[2:]))
	}

	// `npd license ...` is the other subcommand that has to work when the
	// daemon does not: a fresh install with no datastore configured, or a box
	// where the panel will not start. Both are exactly when an operator needs to
	// see or fix the licence, and neither can wait for npd to come up first.
	if len(os.Args) > 1 && os.Args[1] == "license" {
		os.Exit(runLicense(os.Args[2:]))
	}

	var (
		configPath  = flag.String("config", envOr("NP_CONFIG", "/etc/nexpanel/config.yaml"), "path to config file")
		showVersion = flag.Bool("version", false, "print version and exit")
		migrate     = flag.Bool("migrate", false, "run datastore migrations and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("npd", version)
		return
	}

	// The config file is optional: if it is absent we run on defaults + env,
	// which keeps local/dev runs frictionless. But "absent" and "somewhere I am
	// not looking" are the same thing from here, so the path we skipped is kept
	// and reported below: a config file the operator wrote and npd silently
	// ignored is indistinguishable from one that does not exist, and the symptom
	// — a panel that says it has no datastore while a config file plainly sets
	// one — sends you looking at the database instead of at the path.
	path := *configPath
	skipped := ""
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			skipped, path = path, ""
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "npd: config error:", err)
		os.Exit(1)
	}

	log := logx.New(os.Stdout, logx.Options{
		Level:  logx.ParseLevel(cfg.Log.Level),
		Format: logx.Format(cfg.Log.Format),
	})

	if path != "" {
		log.Info("config loaded", "path", path)
	} else {
		log.Info("no config file — running on defaults and NP_* environment variables",
			"looked_for", skipped, "override_with", "-config <path> or NP_CONFIG")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --migrate is a one-shot: open the datastore, apply migrations, and exit.
	// The installer's db.migrate step calls this so the schema is in place before
	// the service starts; it is also handy operationally for an out-of-band
	// upgrade. Running the full daemon migrates on boot anyway (bootstrap.New).
	if *migrate {
		if !repository.Configured(cfg.Database) {
			fmt.Fprintln(os.Stderr, "npd: --migrate requires a configured datastore")
			os.Exit(1)
		}
		db, err := repository.Open(cfg.Database)
		if err != nil {
			log.Error("migrate: open datastore", "err", err)
			os.Exit(1)
		}
		defer func() { _ = db.Close() }()
		applied, err := repository.Migrate(ctx, db)
		if err != nil {
			log.Error("migrate failed", "err", err)
			os.Exit(1)
		}
		log.Info("migrations applied", "count", applied, "dialect", db.Dialect)
		return
	}

	app, err := bootstrap.New(ctx, cfg, log, version)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = app.Close() }()

	if err := app.Run(ctx); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
	log.Info("shutdown complete")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
