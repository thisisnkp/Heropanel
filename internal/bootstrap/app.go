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

	"github.com/thisisnkp/heropanel/internal/apps"
	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	backuppkg "github.com/thisisnkp/heropanel/internal/backup"
	brokerclient "github.com/thisisnkp/heropanel/internal/broker"
	icache "github.com/thisisnkp/heropanel/internal/cache"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/cron"
	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/internal/docker"
	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/internal/files"
	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/monitor"
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

	// Docker. Unlike every other module this one needs no datastore — it manages
	// containers on the host, not rows — but it is useless without the broker,
	// because the daemon socket is root-equivalent and hpd must never hold it.
	// A nil gateway therefore switches the module off entirely rather than
	// leaving a UI that offers containers it cannot reach.
	dockerSvc := docker.New(gw)
	if client, ok := gw.(*brokerclient.Client); ok && client != nil && dockerSvc != nil {
		// Container shells need a *streaming* broker connection, which only the
		// concrete client provides — the same requirement the site terminal has.
		dockerSvc = dockerSvc.WithStreams(client)
	}

	// The one-click app catalog rides on Docker: an app is a labelled compose
	// stack, so it exists exactly when Docker does and adds no privilege.
	var appsSvc *apps.Service
	if dockerSvc != nil {
		appsSvc = apps.New(dockerSvc)
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
	var cronSvc *cron.Service
	var backupSvc *backuppkg.Service
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
			// appsSvc resolves a proxy site's upstream when it is backed by a
			// one-click app. Nil when Docker is absent, in which case an app-backed
			// site simply renders as a static vhost — the same graceful fallback a
			// systemd proxy site has before its runtime exists.
			Apps: appsSvc,
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
		// Scheduled jobs: site-scoped systemd timers. The service resolves a site
		// to its user/home through the same store the other site facets use.
		cronSvc = cron.NewService(repository.NewCronStore(db), cronSiteAdapter{repo: siteStore}, gw)
		// Backups: always sealed before they touch a target. The key is a
		// purpose-derived subkey of the master key, so backups and column secrets
		// can never be confused; no key → the module reports unavailable rather
		// than ever storing a site's data in the clear.
		backupKey, kerr := secrets.DeriveKeyBase64(cfg.Security.SecretKey, "backup-v1")
		if kerr != nil {
			log.Warn("backup key derivation failed — backups disabled", "err", kerr)
		}
		var s3Target backuppkg.Target
		if t := backuppkg.NewS3(backuppkg.S3Config{
			Endpoint: cfg.Backup.S3.Endpoint, Region: cfg.Backup.S3.Region, Bucket: cfg.Backup.S3.Bucket,
			AccessKey: cfg.Backup.S3.AccessKey, SecretKey: cfg.Backup.S3.SecretKey,
		}); t != nil {
			s3Target = t
			log.Info("backup s3 target configured", "endpoint", cfg.Backup.S3.Endpoint, "bucket", cfg.Backup.S3.Bucket)
			// Best-effort: surface a missing bucket (the most common S3
			// misconfiguration) at boot rather than at the first scheduled backup.
			if err := t.EnsureBucket(ctx); err != nil {
				log.Warn("backup s3 bucket check failed", "err", err)
			}
		}
		backupStore := repository.NewBackupStore(db)
		backupSvc = backuppkg.NewService(backupStore, backupSiteAdapter{repo: siteStore}, gw, backupKey, s3Target)
		// The database module lets a site's backup carry its database: a full
		// dump per backup, sealed as a second object on the same target.
		backupSvc = backupSvc.WithDBs(backupDBAdapter{svc: dbSvc, repo: dbStore})
		// Panel self-backup: the panel's own database on the same pipeline,
		// sealed with the same derived key. Restore is out-of-band by design
		// (`hpd decrypt` + docs/22 §7).
		if cfg.Backup.Panel.Enabled {
			backupSvc = backupSvc.WithPanel(backupStore, panelSnapshotter(db, cfg.Database, gw), backuppkg.PanelPolicy{
				Target: cfg.Backup.Panel.Target, IntervalHours: cfg.Backup.Panel.IntervalHours, Keep: cfg.Backup.Panel.Keep,
			})
			if backupSvc.PanelAvailable() {
				go backupSvc.RunPanelScheduler(ctx, log)
				log.Info("panel self-backup enabled",
					"interval_hours", cfg.Backup.Panel.IntervalHours, "target", cfg.Backup.Panel.Target)
			}
		}
		if backupSvc.Available() {
			go backupSvc.RunScheduler(ctx, func(ctx context.Context, id int64) (string, bool) {
				rec, err := siteStore.GetByID(ctx, id)
				if err != nil {
					return "", false
				}
				return rec.UID, true
			}, log)
			log.Info("backup scheduler enabled")
		}
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
			{"scheduler", []string{"cron.jobs", "cron.logs"}, cronSvc != nil},
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

	// Docker registers outside the datastore block because it is the one module
	// that manages the host rather than rows. It advertises itself only when a
	// daemon actually answers, so the UI greys the feature out on a host without
	// Docker instead of offering buttons that cannot work.
	if dockerSvc != nil && dockerSvc.Available(ctx) {
		if err := reg.Register(ctx, registry.NewInCore("docker",
			"docker.containers", "docker.images", "docker.logs", "docker.stats")); err != nil {
			log.Warn("could not register in-core module", "slug", "docker", "err", err)
		}
		log.Info("docker module enabled", "server_version", dockerSvc.Info(ctx).ServerVersion)
	}

	// Monitoring is always available: node metrics come from world-readable /proc,
	// so unlike most modules it needs neither a datastore nor a broker.
	if err := reg.Register(ctx, registry.NewInCore("monitor", "monitor.node")); err != nil {
		log.Warn("could not register in-core module", "slug", "monitor", "err", err)
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

	// Node metrics. Needs no datastore or broker — /proc is world-readable — so
	// the monitor exists whenever hpd does. Per-site and service metrics are wired
	// on top when the pieces they need are present; the live dashboard's sampler
	// is started with the hub below.
	monitorSvc := monitor.New()
	if gw != nil {
		// Service health goes through the broker's read-only service.status.
		monitorSvc = monitorSvc.WithServices(monitor.NewServiceReader(gw, monitor.DefaultServices))
	}
	if db != nil {
		// History and rollups: a raw sample a minute, folded hourly, pruned so the
		// table stays bounded. Persistence is deliberately NOT subscription-gated —
		// a chart that skipped the hours nobody was watching would lie by omission.
		monitorSvc = monitorSvc.WithHistory(repository.NewMetricStore(db))
		// Alert rules: threshold breaches fire notifications and record events. The
		// store seals notification targets with the panel's data key. Evaluation is
		// folded into the persister, so it runs on the same tick.
		alertStore := repository.NewAlertStore(db, cipher)
		monitorSvc = monitorSvc.
			WithAlertAdmin(alertStore).
			WithAlerts(monitor.NewEvaluator(alertStore, monitor.NewHTTPNotifier(log), log))
		// The persist cadence is a minute in production; the e2e shortens it via
		// HP_MONITOR_PERSIST_SEC so a firing can be proven without a real minute.
		persistEvery := time.Duration(0)
		if v := os.Getenv("HP_MONITOR_PERSIST_SEC"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				persistEvery = time.Duration(n) * time.Second
			}
		}
		go monitorSvc.RunPersister(ctx, persistEvery, log)
		go monitorSvc.RunRollup(ctx, log)
	}
	if siteSvc != nil {
		// Per-site metrics need the current site list on each (gated) sweep, so it
		// always reflects the sites that exist right now.
		monitorSvc = monitorSvc.WithSites(func() []monitor.SiteRef {
			sites, err := siteSvc.List(ctx, 0, 500, 0)
			if err != nil {
				return nil
			}
			refs := make([]monitor.SiteRef, 0, len(sites))
			for _, s := range sites {
				if s.SystemUser != "" {
					refs = append(refs, monitor.SiteRef{VhostName: s.SystemUser, SiteUID: s.UID})
				}
			}
			return refs
		})
	}

	// Realtime WebSocket hub. Its local Publish needs no Redis (Redis only bridges
	// cross-process job events), so the hub is created whenever there is a
	// datastore to authenticate subscribers against — the live monitor dashboard
	// then works even on an install without Redis. The channel authorizer gates
	// job channels by ownership and monitor channels by monitor.read; jobs may be
	// nil, in which case job channels simply deny.
	var wsHub *ws.Hub
	if db != nil {
		wsHub = ws.NewHub(cacheWiring.RedisClient, channelAuthorizer(jobs), log)
		go wsHub.Run(ctx)
		// Push node samples to subscribed dashboards, sampling only while watched.
		go monitorSvc.RunSampler(ctx, wsHub, monitor.DefaultInterval, log)
		log.Info("realtime hub enabled", "redis_bridge", cacheWiring.RedisClient != nil)
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
		Docker:     dockerSvc,
		Apps:       appsSvc,
		Files:      filesSvc,
		Terminal:   terminalSvc,
		Recordings: recordings,
		Runtime:    runtimeSvc,
		Cron:       cronSvc,
		Backups:    backupSvc,
		Monitor:    monitorSvc,
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
