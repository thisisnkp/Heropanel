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
	"github.com/thisisnkp/heropanel/pkg/logx"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	var (
		configPath  = flag.String("config", envOr("HP_CONFIG", "/etc/heropanel/config.yaml"), "path to config file")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("hpd", version)
		return
	}

	// The config file is optional: if it is absent we run on defaults + env,
	// which keeps local/dev runs frictionless.
	path := *configPath
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			path = ""
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
