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
	"os"
	"strconv"
	"time"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	brokerclient "github.com/thisisnkp/heropanel/internal/broker"
	icache "github.com/thisisnkp/heropanel/internal/cache"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/internal/files"
	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/registry"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/runtime"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/ssl"
	"github.com/thisisnkp/heropanel/internal/terminal"
	"github.com/thisisnkp/heropanel/internal/webserver"
	"github.com/thisisnkp/heropanel/internal/ws"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
	"github.com/thisisnkp/heropanel/pkg/idgen"
	"github.com/thisisnkp/heropanel/pkg/secrets"
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
		// This is not a normal degraded mode: with no datastore there is no auth
		// service, so nobody can log in and no feature works. Say so plainly and
		// say how to fix it — the alternative is an operator staring at a login
		// screen that rejects every attempt.
		log.Error("no datastore configured — the panel cannot sign anyone in",
			"fix", "set database.dsn in the config file, or the HP_DATABASE_DSN environment variable, then restart",
			"example_sqlite", "HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/var/lib/heropanel/hp.db")
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

	// The master key that seals the *_enc columns (Git credentials today). An
	// operator who has not set one gets a working panel minus the features that
	// must store a secret at rest — those report "unavailable" rather than
	// silently keeping the secret in the clear.
	cipher, err := secrets.FromBase64(cfg.Security.SecretKey)
	if err != nil {
		_ = cacheWiring.Close()
		_ = l1.Close()
		if db != nil {
			_ = db.Close()
		}
		return nil, fmt.Errorf("bootstrap: secret key: %w", err)
	}
	if cipher.Configured() {
		log.Info("secret encryption enabled")
	} else {
		log.Warn("no security.secret_key set — private Git repositories are disabled")
	}

	// Services are available only with a datastore. Seed baseline RBAC
	// (idempotent), then construct the auth and site services.
	var authSvc *auth.Service
	var auditSvc *audit.Service
	var userDir httpapi.UserDirectory
	var siteSvc *site.Service
	var phpSvc *php.Service
	var dbSvc *database.Service
	var sslSvc *ssl.Service
	var gitSvc *git.Service
	var filesSvc *files.Service
	var terminalSvc *terminal.Service
	var recordings *terminal.RecordingStore
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
		auditSvc = audit.NewService(repository.NewAuditRepository(db))
		userDir = &userDirectoryAdapter{repo: users}
		siteStore := repository.NewSiteStore(db)
		runtimeSvc = runtime.NewService(repository.NewRuntimeStore(db), runtimeSiteAdapter{repo: siteStore}, gw)
		domainSvc = domain.NewService(repository.NewDomainStore(db), domainSiteAdapter{repo: siteStore})
		phpSvc = php.NewService(repository.NewPHPPoolStore(db), gw)
		siteSvc = site.NewService(site.Deps{
			Repo:    siteStore,
			Broker:  gw,
			Web:     webserver.NewService(gw),
			PHP:     phpSvc,
			Runtime: siteRuntimeAdapter{svc: runtimeSvc},
			Domains: siteDomainsAdapter{svc: domainSvc},
		})
		// The runtime re-renders the vhost (as a proxy) after a runtime change;
		// the domain service re-renders it after an alias/redirect/force-HTTPS change.
		runtimeSvc.WithReproxy(siteSvc.ReapplyWebserver)
		domainSvc.WithReapply(siteSvc.ReapplyWebserver)
		dbStore := repository.NewDatabaseStore(db)
		dbSvc = database.NewService(dbStore, gw)
		if cfg.Database.AdminerURL != "" {
			// Hand-off signs in with a throwaway account, so it needs somewhere to
			// record what to drop later.
			dbSvc.WithAdminer(cfg.Database.AdminerURL, dbStore)
			log.Info("database client hand-off enabled", "url", cfg.Database.AdminerURL)
		}
		dnsSvc = dns.NewService(repository.NewDNSStore(db), gw)
		gitSvc = git.NewService(repository.NewGitStore(db), gitSiteAdapter{repo: siteStore}, gw).
			WithRestarter(runtimeSvc). // auto-restart a proxy app after each deploy
			WithSecrets(cipher)        // enables private repos (token / deploy key)

		// The File Manager needs the privileged broker to act as the site's Linux
		// user; it is baremetal-only (the gate lives in the service). Wired even
		// when the broker is absent — its calls then report "unavailable" rather
		// than the feature vanishing from the UI mid-session.
		filesSvc = files.NewService(filesSiteAdapter{repo: siteStore}, gw)

		// The web terminal needs a *streaming* broker connection, which only the
		// concrete client provides. Without a broker there is no way to run a
		// shell as another user, so the feature stays switched off rather than
		// offering a terminal that cannot open.
		if client, ok := gw.(*brokerclient.Client); ok && client != nil {
			terminalSvc = terminal.NewService(terminalSiteAdapter{repo: siteStore}, client)
		}

		// Session recording. The transcript files live on disk; only their
		// metadata is in the database. An unwritable directory disables recording
		// rather than the terminal — a shell the operator asked for must not fail
		// because its audit artifact could not be stored — but it is logged at
		// ERROR, because a panel that believes it is recording and is not is worse
		// than one that never claimed to.
		if dir := cfg.Terminal.Recording.Dir; dir != "" {
			retention := time.Duration(cfg.Terminal.Recording.RetentionDays) * 24 * time.Hour
			if err := os.MkdirAll(dir, 0o750); err != nil {
				log.Error("terminal session recording is DISABLED: the recordings directory could not be created",
					"dir", dir, "err", err,
					"fix", "create the directory and make it writable by the hpd user, or set terminal.recording.dir to \"\" to silence this")
			} else {
				recordings = terminal.NewRecordingStore(dir, repository.NewRecordingStore(db), retention)
				log.Info("terminal session recording enabled",
					"dir", dir, "retention_days", cfg.Terminal.Recording.RetentionDays)
			}
		}

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

	// The module registry. In-core features register the capabilities they
	// provide — but only the ones actually wired (their datastore is present) —
	// so the set the UI gates on reflects what this hpd can really do, not what
	// the binary was compiled with. Satellite modules (Phase 9/10) will Register
	// here at enable time over the same interface.
	reg := registry.New()
	if db != nil {
		type incore struct {
			slug string
			caps []string
			on   bool
		}
		for _, m := range []incore{
			{"sites", []string{"site.manage", "site.php", "site.limits", "site.logs"}, siteSvc != nil},
			{"php", []string{"php.pool", "php.extensions"}, phpSvc != nil},
			{"databases", []string{"database.manage", "database.export", "database.adminer"}, dbSvc != nil},
			{"git", []string{"git.deploy", "git.rollback"}, gitSvc != nil},
			{"files", []string{"file.browse", "file.edit", "file.upload"}, filesSvc != nil},
			{"terminal", []string{"terminal.session"}, terminalSvc != nil && terminalSvc.Available()},
			{"runtime", []string{"runtime.app", "runtime.health"}, runtimeSvc != nil},
			{"ssl", []string{"ssl.issue", "ssl.dns01"}, sslSvc != nil},
			{"dns", []string{"dns.zone", "dns.record"}, dnsSvc != nil},
			{"domains", []string{"domain.alias", "domain.redirect"}, domainSvc != nil},
			{"audit", []string{"audit.read", "audit.verify"}, auditSvc != nil},
		} {
			if !m.on {
				continue
			}
			if err := reg.Register(ctx, registry.NewInCore(m.slug, m.caps...)); err != nil {
				log.Warn("could not register in-core module", "slug", m.slug, "err", err)
			}
		}
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
		d.Register("site.clone", func(ctx context.Context, j *job.Job, p job.Progress) (any, error) {
			var in site.CloneInput
			if err := json.Unmarshal(j.Payload, &in); err != nil {
				return nil, err
			}
			s, err := siteSvc.RunClone(ctx, in, p)
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

	// Drops the throwaway accounts minted for Adminer hand-offs once they expire.
	// It also sweeps on startup, cleaning up after a restart mid-session.
	if dbSvc != nil && cfg.Database.AdminerURL != "" {
		if recordings != nil {
			// Retention is not optional housekeeping: it is the half of the policy
			// that says the panel stops holding a transcript of someone's work.
			go recordings.RunPurger(ctx, log)
		}
		go dbSvc.RunSSOSweeper(ctx, log)
		log.Info("database sign-on sweeper enabled", "ttl", database.SSOTTL.String())
	}

	// Realtime WebSocket hub: bridges Redis Pub/Sub job events to browsers.
	var wsHub *ws.Hub
	if jobs != nil {
		wsHub = ws.NewHub(cacheWiring.RedisClient, jobChannelAuthorizer(jobs), log)
		go wsHub.Run(ctx)
		log.Info("realtime hub enabled")
	}

	handler := httpapi.NewRouter(httpapi.Deps{
		Ctx:        ctx,
		Config:     cfg,
		Logger:     log,
		Version:    version,
		StartedAt:  time.Now(),
		DB:         dbHealth,
		Redis:      redisHealth,
		Broker:     brokerHealth,
		Auth:       authSvc,
		Audit:      auditSvc,
		Users:      userDir,
		Sites:      siteSvc,
		PHP:        phpSvc,
		Databases:  dbSvc,
		SSL:        sslSvc,
		DNS:        dnsSvc,
		Domains:    domainSvc,
		Git:        gitSvc,
		Files:      filesSvc,
		Terminal:   terminalSvc,
		Recordings: recordings,
		Runtime:    runtimeSvc,
		Jobs:       jobs,
		Registry:   reg,
		WS:         wsHub,
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
