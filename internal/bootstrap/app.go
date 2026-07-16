// Package bootstrap is hpd's composition root. It wires configuration, logging,
// and the HTTP server together (dependency injection via explicit constructors,
// docs/01 §2) and owns process lifecycle: start, serve, graceful shutdown.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/thisisnkp/heropanel/internal/auth"
	brokerclient "github.com/thisisnkp/heropanel/internal/broker"
	icache "github.com/thisisnkp/heropanel/internal/cache"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/runtime"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/ssl"
	"github.com/thisisnkp/heropanel/internal/webserver"
	"github.com/thisisnkp/heropanel/internal/ws"
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
	var dbHealth, redisHealth, brokerHealth httpapi.HealthChecker
	if db != nil {
		dbHealth = db
	}
	if cacheWiring.RedisHealth != nil {
		redisHealth = cacheWiring.RedisHealth
	}

	// Broker client (opt-in): only when a socket is configured. It is the gateway
	// through which services request privileged operations. Left nil when the
	// broker is absent (services that need it fail with an "unavailable" error).
	var gw brokerclient.Gateway
	if cfg.Broker.Socket != "" {
		client := brokerclient.NewClient(cfg.Broker.Socket, cfg.Broker.Token, log)
		gw = client
		brokerHealth = client
		log.Info("broker gateway configured", "socket", cfg.Broker.Socket)
	}

	// Services are available only with a datastore. Seed baseline RBAC
	// (idempotent), then construct the auth and site services.
	var authSvc *auth.Service
	var userDir httpapi.UserDirectory
	var siteSvc *site.Service
	var dbSvc *database.Service
	var sslSvc *ssl.Service
	var gitSvc *git.Service
	var runtimeSvc *runtime.Service
	var dnsSvc *dns.Service
	var domainSvc *domain.Service
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
		authSvc = auth.NewService(users, sessions, rbac, cacheWiring.Cache, auth.DefaultConfig()).
			WithAPIKeys(repository.NewAPIKeyRepository(db))
		userDir = &userDirectoryAdapter{repo: users}
		siteStore := repository.NewSiteStore(db)
		runtimeSvc = runtime.NewService(repository.NewRuntimeStore(db), runtimeSiteAdapter{repo: siteStore}, gw)
		domainSvc = domain.NewService(repository.NewDomainStore(db), domainSiteAdapter{repo: siteStore})
		siteSvc = site.NewService(site.Deps{
			Repo:    siteStore,
			Broker:  gw,
			Web:     webserver.NewService(gw),
			PHP:     php.NewService(repository.NewPHPPoolStore(db), gw),
			Runtime: runtimeSvc,
			Domains: siteDomainsAdapter{svc: domainSvc},
		})
		// The runtime re-renders the vhost (as a proxy) after a runtime change;
		// the domain service re-renders it after an alias/redirect/force-HTTPS change.
		runtimeSvc.WithReproxy(siteSvc.ReapplyWebserver)
		domainSvc.WithReapply(siteSvc.ReapplyWebserver)
		dbSvc = database.NewService(repository.NewDatabaseStore(db), gw)
		dnsSvc = dns.NewService(repository.NewDNSStore(db), gw)
		gitSvc = git.NewService(repository.NewGitStore(db), gitSiteAdapter{repo: siteStore}, gw).
			WithRestarter(runtimeSvc) // auto-restart a proxy app after each deploy

		// SSL: self-signed and custom uploads always available; Let's Encrypt
		// (ACME) enabled only when an account email is configured.
		var acmeProvider ssl.ACME
		if cfg.SSL.Email != "" {
			if le, err := ssl.NewLetsEncrypt(cfg.SSL.Directory, cfg.SSL.Email); err != nil {
				log.Warn("could not initialize Let's Encrypt", "err", err)
			} else {
				acmeProvider = le
				log.Info("Let's Encrypt enabled", "email", cfg.SSL.Email)
			}
		}
		sslSvc = ssl.NewService(repository.NewCertStore(db), gw, acmeProvider).
			WithDNS(sslDNSAdapter{svc: dnsSvc}) // enables DNS-01 + wildcard issuance

		log.Info("auth ready", "session_ttl", auth.DefaultConfig().SessionTTL.String())
	}

	// Async job queue (requires a datastore and Redis). When absent, site
	// operations run synchronously in the request.
	var jobs *job.Dispatcher
	if db != nil && cacheWiring.RedisClient != nil {
		d := job.NewDispatcher(repository.NewJobStore(db), cacheWiring.RedisClient, log)
		d.Register("site.create", func(ctx context.Context, j *job.Job, p job.Progress) (any, error) {
			var in site.CreateInput
			if err := json.Unmarshal(j.Payload, &in); err != nil {
				return nil, err
			}
			s, err := siteSvc.RunCreate(ctx, in, p)
			if err != nil {
				return nil, err
			}
			return map[string]any{"site_uid": s.UID}, nil
		})
		d.Register("site.delete", func(ctx context.Context, j *job.Job, p job.Progress) (any, error) {
			var pl struct {
				UID string `json:"uid"`
			}
			if err := json.Unmarshal(j.Payload, &pl); err != nil {
				return nil, err
			}
			return nil, siteSvc.RunDelete(ctx, pl.UID, p)
		})
		d.Register("git.deploy", func(ctx context.Context, j *job.Job, p job.Progress) (any, error) {
			var pl struct {
				SiteUID string `json:"site_uid"`
				Trigger string `json:"trigger"`
			}
			if err := json.Unmarshal(j.Payload, &pl); err != nil {
				return nil, err
			}
			dep, err := gitSvc.RunDeploy(ctx, pl.SiteUID, pl.Trigger, p)
			if err != nil {
				return nil, err
			}
			return map[string]any{"deployment_uid": dep.UID, "commit": dep.CommitSHA}, nil
		})
		d.Register("git.rollback", func(ctx context.Context, j *job.Job, p job.Progress) (any, error) {
			var pl struct {
				SiteUID       string `json:"site_uid"`
				DeploymentUID string `json:"deployment_uid"`
			}
			if err := json.Unmarshal(j.Payload, &pl); err != nil {
				return nil, err
			}
			dep, err := gitSvc.RunRollback(ctx, pl.SiteUID, pl.DeploymentUID, p)
			if err != nil {
				return nil, err
			}
			return map[string]any{"deployment_uid": dep.UID}, nil
		})
		if err := d.StartWorkers(ctx, 2); err != nil {
			log.Warn("job workers failed to start; falling back to synchronous operations", "err", err)
		} else {
			jobs = d
			log.Info("job queue enabled")
		}
	}

	// Certificate auto-renewal: sweeps for certs nearing expiry and re-issues
	// them with the flow that created them (HTTP-01, DNS-01/wildcard, or a fresh
	// self-signed). Uploaded certs are left alone.
	if sslSvc != nil {
		go ssl.NewRenewer(sslSvc, log).Run(ctx)
		log.Info("certificate renewer enabled",
			"interval", ssl.DefaultRenewInterval.String(), "window", ssl.DefaultRenewWindow.String())
	}

	// Realtime WebSocket hub: bridges Redis Pub/Sub job events to browsers.
	var wsHub *ws.Hub
	if jobs != nil {
		wsHub = ws.NewHub(cacheWiring.RedisClient, jobChannelAuthorizer(jobs), log)
		go wsHub.Run(ctx)
		log.Info("realtime hub enabled")
	}

	handler := httpapi.NewRouter(httpapi.Deps{
		Ctx:       ctx,
		Config:    cfg,
		Logger:    log,
		Version:   version,
		StartedAt: time.Now(),
		DB:        dbHealth,
		Redis:     redisHealth,
		Broker:    brokerHealth,
		Auth:      authSvc,
		Users:     userDir,
		Sites:     siteSvc,
		Databases: dbSvc,
		SSL:       sslSvc,
		DNS:       dnsSvc,
		Domains:   domainSvc,
		Git:       gitSvc,
		Runtime:   runtimeSvc,
		Jobs:      jobs,
		WS:        wsHub,
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
