// Command hpd is the HeroPanel core control-plane daemon: the unprivileged,
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

	"github.com/thisisnkp/heropanel/internal/bootstrap"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/logx"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	var (
		configPath  = flag.String("config", envOr("HP_CONFIG", "/etc/heropanel/config.yaml"), "path to config file")
		showVersion = flag.Bool("version", false, "print version and exit")
		migrate     = flag.Bool("migrate", false, "run datastore migrations and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("hpd", version)
		return
	}

	// The config file is optional: if it is absent we run on defaults + env,
	// which keeps local/dev runs frictionless. But "absent" and "somewhere I am
	// not looking" are the same thing from here, so the path we skipped is kept
	// and reported below: a config file the operator wrote and hpd silently
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
		fmt.Fprintln(os.Stderr, "hpd: config error:", err)
		os.Exit(1)
	}

	log := logx.New(os.Stdout, logx.Options{
		Level:  logx.ParseLevel(cfg.Log.Level),
		Format: logx.Format(cfg.Log.Format),
	})

	if path != "" {
		log.Info("config loaded", "path", path)
	} else {
		log.Info("no config file — running on defaults and HP_* environment variables",
			"looked_for", skipped, "override_with", "-config <path> or HP_CONFIG")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --migrate is a one-shot: open the datastore, apply migrations, and exit.
	// The installer's db.migrate step calls this so the schema is in place before
	// the service starts; it is also handy operationally for an out-of-band
	// upgrade. Running the full daemon migrates on boot anyway (bootstrap.New).
	if *migrate {
		if !repository.Configured(cfg.Database) {
			fmt.Fprintln(os.Stderr, "hpd: --migrate requires a configured datastore")
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
