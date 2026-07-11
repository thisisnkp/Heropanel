// Package bootstrap is hpd's composition root. It wires configuration, logging,
// and the HTTP server together (dependency injection via explicit constructors,
// docs/01 §2) and owns process lifecycle: start, serve, graceful shutdown.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/thisisnkp/heropanel/internal/auth"
	icache "github.com/thisisnkp/heropanel/internal/cache"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/repository"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// App holds the wired application, its HTTP server, and owned resources.
type App struct {
	cfg        config.Config
	log        *slog.Logger
	srv        *http.Server
	db         *repository.DB // may be nil when no datastore is configured
	l1         *pcache.LocalCache
	cache      pcache.Cache // composed two-tier cache (consumed by services)
	cacheClose func() error
}

// New builds the App: it makes the given logger the process default, opens and
// migrates the datastore (if configured), and constructs the HTTP server from
// the router. ctx is the lifecycle context used by background helpers (e.g. the
// rate-limiter janitor).
func New(ctx context.Context, cfg config.Config, log *slog.Logger, version string) (*App, error) {
	slog.SetDefault(log)

	var db *repository.DB
	if repository.Configured(cfg.Database) {
		opened, err := repository.Open(cfg.Database)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: %w", err)
		}
		applied, err := repository.Migrate(ctx, opened)
		if err != nil {
			_ = opened.Close()
			return nil, fmt.Errorf("bootstrap: migrate: %w", err)
		}
		log.Info("database ready", "dialect", opened.Dialect, "migrations_applied", applied)
		db = opened
	} else {
		log.Warn("no datastore configured (empty DSN); running without persistence")
	}

	// Two-tier cache: an always-present in-process L1, composed with Redis L2 +
	// invalidation bus when Redis is configured (else L1-only).
	l1 := pcache.NewLocal(pcache.LocalConfig{MaxEntries: 10000, JanitorInterval: time.Minute})
	cacheWiring, err := icache.Build(ctx, cfg.Redis, l1, idgen.NewULID(), log)
	if err != nil {
		_ = l1.Close()
		if db != nil {
			_ = db.Close()
		}
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	// Avoid the typed-nil interface gotcha: only set a HealthChecker when the
	// concrete dependency exists, so /readyz reports "not_configured" cleanly.
	var dbHealth, redisHealth httpapi.HealthChecker
	if db != nil {
		dbHealth = db
	}
	if cacheWiring.RedisHealth != nil {
		redisHealth = cacheWiring.RedisHealth
	}

	// Auth/RBAC is available only with a datastore. Seed baseline roles and
	// permissions (idempotent), then construct the auth service.
	var authSvc *auth.Service
	var userDir httpapi.UserDirectory
	if db != nil {
		users := repository.NewUserRepository(db)
		sessions := repository.NewSessionRepository(db)
		rbac := repository.NewRBACRepository(db)
		if err := auth.SeedRBAC(ctx, rbac); err != nil {
			_ = cacheWiring.Close()
			_ = l1.Close()
			_ = db.Close()
			return nil, fmt.Errorf("bootstrap: seed rbac: %w", err)
		}
		authSvc = auth.NewService(users, sessions, rbac, cacheWiring.Cache, auth.DefaultConfig())
		userDir = &userDirectoryAdapter{repo: users}
		log.Info("auth ready", "session_ttl", auth.DefaultConfig().SessionTTL.String())
	}

	handler := httpapi.NewRouter(httpapi.Deps{
		Ctx:       ctx,
		Config:    cfg,
		Logger:    log,
		Version:   version,
		StartedAt: time.Now(),
		DB:        dbHealth,
		Redis:     redisHealth,
		Auth:      authSvc,
		Users:     userDir,
	})

	srv := &http.Server{
		Addr:         net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)),
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout.D(),
		WriteTimeout: cfg.Server.WriteTimeout.D(),
		IdleTimeout:  cfg.Server.IdleTimeout.D(),
		ErrorLog:     slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	return &App{
		cfg:        cfg,
		log:        log,
		srv:        srv,
		db:         db,
		l1:         l1,
		cache:      cacheWiring.Cache,
		cacheClose: cacheWiring.Close,
	}, nil
}

// Close releases owned resources (Redis client, L1 cache, datastore). Call after
// Run returns.
func (a *App) Close() error {
	if a.cacheClose != nil {
		_ = a.cacheClose()
	}
	if a.l1 != nil {
		_ = a.l1.Close()
	}
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Run serves until ctx is cancelled (e.g. SIGINT/SIGTERM) or the server fails,
// then drains in-flight requests within the shutdown timeout.
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		a.log.Info("http server listening", "addr", a.srv.Addr, "tls", a.cfg.Server.TLS.Enabled)
		var err error
		if a.cfg.Server.TLS.Enabled {
			err = a.srv.ListenAndServeTLS(a.cfg.Server.TLS.CertFile, a.cfg.Server.TLS.KeyFile)
		} else {
			err = a.srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		a.log.Info("shutdown signal received, draining connections")
		shCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.ShutdownTimeout.D())
		defer cancel()
		return a.srv.Shutdown(shCtx)
	}
}
