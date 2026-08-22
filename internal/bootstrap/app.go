// Package bootstrap is npd's composition root. It wires configuration, logging,
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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/internal/apps"
	"github.com/thisisnkp/nexpanel/internal/audit"
	"github.com/thisisnkp/nexpanel/internal/auth"
	"github.com/thisisnkp/nexpanel/internal/auth/webauthn"
	backuppkg "github.com/thisisnkp/nexpanel/internal/backup"
	brokerclient "github.com/thisisnkp/nexpanel/internal/broker"
	icache "github.com/thisisnkp/nexpanel/internal/cache"
	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/cron"
	"github.com/thisisnkp/nexpanel/internal/database"
	"github.com/thisisnkp/nexpanel/internal/dns"
	"github.com/thisisnkp/nexpanel/internal/docker"
	"github.com/thisisnkp/nexpanel/internal/domain"
	"github.com/thisisnkp/nexpanel/internal/files"
	"github.com/thisisnkp/nexpanel/internal/git"
	"github.com/thisisnkp/nexpanel/internal/httpapi"
	"github.com/thisisnkp/nexpanel/internal/job"
	"github.com/thisisnkp/nexpanel/internal/keyring"
	mailpkg "github.com/thisisnkp/nexpanel/internal/mail"
	"github.com/thisisnkp/nexpanel/internal/marketplace"
	"github.com/thisisnkp/nexpanel/internal/monitor"
	"github.com/thisisnkp/nexpanel/internal/php"
	"github.com/thisisnkp/nexpanel/internal/registry"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/internal/runtime"
	"github.com/thisisnkp/nexpanel/internal/safe"
	"github.com/thisisnkp/nexpanel/internal/security"
	"github.com/thisisnkp/nexpanel/internal/setup"
	"github.com/thisisnkp/nexpanel/internal/site"
	"github.com/thisisnkp/nexpanel/internal/ssl"
	"github.com/thisisnkp/nexpanel/internal/systemd"
	"github.com/thisisnkp/nexpanel/internal/tenancy"
	"github.com/thisisnkp/nexpanel/internal/terminal"
	"github.com/thisisnkp/nexpanel/internal/update"
	usermgmt "github.com/thisisnkp/nexpanel/internal/users"
	"github.com/thisisnkp/nexpanel/internal/webhook"
	"github.com/thisisnkp/nexpanel/internal/webmail"
	"github.com/thisisnkp/nexpanel/internal/webserver"
	"github.com/thisisnkp/nexpanel/internal/ws"
	pcache "github.com/thisisnkp/nexpanel/pkg/cache"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
	"github.com/thisisnkp/nexpanel/pkg/secrets"
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
			"fix", "set database.dsn in the config file, or the NP_DATABASE_DSN environment variable, then restart",
			"example_sqlite", "NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=/var/lib/nexpanel/np.db")
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
	var brokerConn *brokerclient.Client
	if cfg.Broker.Remote.Enabled() {
		// A remote broker replaces the socket rather than supplementing it: the
		// panel drives one broker, and quietly falling back to a local one when
		// the remote is misconfigured would run privileged operations on the
		// wrong machine.
		conn, err := remoteBrokerClient(cfg, log)
		if err != nil {
			return nil, err
		}
		brokerConn = conn
		gw = brokerclient.NewResilient(brokerConn, log)
		brokerHealth = brokerConn
		log.Info("broker gateway configured (remote node)", "addr", cfg.Broker.Remote.Addr)
	} else if cfg.Broker.Socket != "" {
		brokerConn = brokerclient.NewClient(cfg.Broker.Socket, cfg.Broker.Token, log)
		// Wrap the raw client in a bulkhead + circuit breaker so a hung or down
		// broker cannot exhaust request goroutines or cost every call a full
		// dial+timeout. Services see the resilient Gateway; the streaming paths
		// below still use the concrete client (a breaker cannot wrap a stream).
		gw = brokerclient.NewResilient(brokerConn, log)
		brokerHealth = brokerConn
		log.Info("broker gateway configured", "socket", cfg.Broker.Socket)
	}

	// Docker. Unlike every other module this one needs no datastore — it manages
	// containers on the host, not rows — but it is useless without the broker,
	// because the daemon socket is root-equivalent and npd must never hold it.
	// A nil gateway therefore switches the module off entirely rather than
	// leaving a UI that offers containers it cannot reach.
	dockerSvc := docker.New(gw)
	if brokerConn != nil && dockerSvc != nil {
		// Container shells need a *streaming* broker connection, which only the
		// concrete client provides — the same requirement the site terminal has.
		dockerSvc = dockerSvc.WithStreams(brokerConn)
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

	// Load the rotating data-key envelope: with a datastore + master key, any
	// wrapped data keys are unwrapped so existing np2 blobs open and new writes
	// seal under the active generation. An empty table leaves legacy np1 mode.
	var keyringSvc *keyring.Service
	if db != nil && cipher.Configured() {
		ks := repository.NewKeyringStore(db)
		if wrapped, kerr := ks.List(ctx); kerr != nil {
			log.Warn("could not load the data-key ring", "err", kerr)
		} else if lerr := cipher.LoadKeyring(wrapped); lerr != nil {
			log.Warn("could not install the data-key ring", "err", lerr)
		} else if len(wrapped) > 0 {
			log.Info("data-key ring loaded", "active_generation", cipher.ActiveGeneration(), "keys", len(wrapped))
		}
		keyringSvc = keyring.NewService(cipher, ks)
	}

	// Services are available only with a datastore. Seed baseline RBAC
	// (idempotent), then construct the auth and site services.
	var authSvc *auth.Service
	var auditSvc *audit.Service
	var userDir httpapi.UserDirectory
	var userMgmt *usermgmt.Service
	var tenancyResolver *tenancy.Resolver
	var webhookSvc *webhook.Service
	var webhookStore *repository.WebhookStore
	var marketplaceSvc *marketplace.Service
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
	var mailSvc *mailpkg.Service
	var webmailSvc *webmail.Service
	var firewallSvc *security.Firewall
	var malwareSvc *security.Malware
	var fail2banSvc *security.Fail2Ban
	var sshSvc *security.SSH
	var updatesSvc *security.Updates
	var fimSvc *security.FIM
	var auditScanSvc *security.Audit
	var dnsSvc *dns.Service
	var domainSvc *domain.Service
	var setupSvc *setup.Service
	var updateSvc *update.Service
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
		// Passkeys (WebAuthn): enabled only when a relying-party id is configured
		// — it must match the panel's domain exactly and cannot be guessed.
		if cfg.Security.WebAuthn.RPID != "" {
			authSvc = authSvc.WithWebAuthn(repository.NewWebAuthnRepository(db), webauthn.New(webauthn.Config{
				RPID:   cfg.Security.WebAuthn.RPID,
				RPName: firstNonEmpty(cfg.Security.WebAuthn.RPName, "NexPanel"),
				Origin: cfg.Security.WebAuthn.Origin,
			}))
			log.Info("passkeys enabled", "rp_id", cfg.Security.WebAuthn.RPID)
		}
		auditSvc = audit.NewService(repository.NewAuditRepository(db))
		userDir = &userDirectoryAdapter{repo: users}
		userMgmt = usermgmt.NewService(users, rbac, sessions)
		tenancyResolver = tenancy.NewResolver(users, repository.NewResourceOwnerStore(db))
		// Outbound webhooks tap the audit stream. They need the data key to seal
		// signing secrets, so they are only enabled when a cipher is configured.
		if cipher.Configured() {
			webhookStore = repository.NewWebhookStore(db, cipher)
			webhookSvc = webhook.NewService(webhookStore, tenancyResolver, rbac, log)
			auditSvc = auditSvc.WithObserver(webhookSvc.OnAuditEntry)
		}
		// Module marketplace: a signed catalog + a pinned publisher keyring over
		// the installed-module store. Keys and the catalog path come from config;
		// an empty keyring trusts nothing, so install stays refused until the
		// operator pins a key. Invalid config degrades to inert rather than fatal —
		// a bad key must not keep the panel from booting.
		mktKeyring, kerr := marketplace.NewKeyring(cfg.Marketplace.Keys...)
		if kerr != nil {
			log.Warn("marketplace: ignoring invalid publisher key(s)", "err", kerr)
			mktKeyring, _ = marketplace.NewKeyring()
		}
		var mktCatalog *marketplace.Catalog
		if cfg.Marketplace.Catalog != "" {
			if c, cerr := marketplace.LoadCatalog(cfg.Marketplace.Catalog); cerr != nil {
				log.Warn("marketplace: could not load catalog", "path", cfg.Marketplace.Catalog, "err", cerr)
			} else {
				mktCatalog = c
			}
		}
		marketplaceSvc = marketplace.NewService(mktKeyring, mktCatalog, repository.NewModuleStore(db), log)
		if ids := mktKeyring.IDs(); len(ids) > 0 {
			log.Info("module marketplace ready", "publisher_keys", ids)
		}
		siteStore := repository.NewSiteStore(db)
		runtimeSvc = runtime.NewService(repository.NewRuntimeStore(db), runtimeSiteAdapter{repo: siteStore}, gw)
		domainSvc = domain.NewService(repository.NewDomainStore(db), domainSiteAdapter{repo: siteStore})
		// Parked-domain registry (park a domain with no site, prove ownership via
		// DNS TXT, offer it as "free" at site creation). Reuses the mail module's
		// resolver pin (cfg.Mail.Resolver / NP_MAIL_RESOLVER) rather than adding a
		// second identical knob — both are the same "pin DNS for e2e" concern.
		domainSvc.WithParked(repository.NewParkedDomainStore(db), cfg.Mail.Resolver)
		phpSvc = php.NewService(repository.NewPHPPoolStore(db), gw)
		// The web-server service renders vhosts for the active engine (OLS by
		// default; switched to the operator's setup choice below).
		webSvc := webserver.NewService(gw)
		siteSvc = site.NewService(site.Deps{
			Repo:    siteStore,
			Broker:  gw,
			Web:     webSvc,
			PHP:     phpSvc,
			Runtime: siteRuntimeAdapter{svc: runtimeSvc},
			Domains: siteDomainsAdapter{svc: domainSvc},
			// appsSvc resolves a proxy site's upstream when it is backed by a
			// one-click app. Nil when Docker is absent, in which case an app-backed
			// site simply renders as a static vhost — the same graceful fallback a
			// systemd proxy site has before its runtime exists.
			Apps: appsSvc,
			// domainSvc's Classify method already matches site.DomainRegistry
			// structurally — no adapter needed.
			Registry: domainSvc,
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

		// First-run setup wizard. With the broker present the operator's
		// webserver/database/DNS/mail choices are provisioned on the host (packages
		// installed, services enabled) via the broker's system.provision
		// capability; without a broker it is record-only (choices persist and gate
		// the panel, but nothing is applied to the host).
		//
		// applyStack points the live services at a selection. The only stack choice
		// left is the web-server engine — OpenLiteSpeed, or the licensed LiteSpeed
		// Enterprise — which changes how vhosts render. The database engine is not
		// a choice: MariaDB is the only one the panel manages. This runs at boot for
		// an already-completed setup, and again from the completion hook so
		// finishing the wizard takes effect without a restart.
		applyStack := func(ctx context.Context, sel setup.Selection) {
			webSvc.SetEngine(webserver.Engine(sel.Webserver))
		}
		var setupProv setup.Provisioner
		if gw != nil {
			// The sign-on script POSTs tickets back to npd over loopback. The
			// address comes from the panel's own listener rather than a second
			// setting, so the two cannot disagree.
			redeem := fmt.Sprintf("http://127.0.0.1:%d/api/v1/databases/sso/redeem", cfg.Server.Port)
			setupProv = setup.NewBrokerProvisioner(gw).WithLogger(log).WithRedeemURL(redeem)
		}
		setupSvc = setup.NewService(repository.NewSetupStore(db), setupProv, log).
			WithOnComplete(func(ctx context.Context, sel setup.Selection) {
				applyStack(ctx, sel)
				// Re-render every vhost in the newly chosen engine's format.
				if err := siteSvc.ReapplyWebserver(ctx); err != nil {
					log.Warn("setup: could not re-apply web server after stack change", "err", err)
				}
				ensureTempDomainWildcard(ctx, dnsSvc, sel, log)
				log.Info("setup completed; hosting stack switched",
					"webserver", sel.Webserver, "db_engine", sel.DBEngine)
			})
		// Adopt an already-completed setup at boot so the engine survives a restart.
		if st, serr := setupSvc.Status(ctx); serr == nil && st.Completed {
			applyStack(ctx, st.Selection)
			log.Info("setup adopted", "webserver", st.Webserver, "db_engine", st.DBEngine)
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
		if cfg.Backup.SweepIntervalSec > 0 {
			backupSvc = backupSvc.WithSweepInterval(time.Duration(cfg.Backup.SweepIntervalSec) * time.Second)
		}
		// SFTP target: a sealed off-cloud copy over SSH (3-2-1). Registered only
		// when a host is configured; credentials come from the secret env.
		if cfg.Backup.SFTP.Host != "" {
			backupSvc = backupSvc.WithTarget(backuppkg.TargetSFTP, backuppkg.NewSFTPTarget(backuppkg.SFTPConfig{
				Host: cfg.Backup.SFTP.Host, Port: cfg.Backup.SFTP.Port, User: cfg.Backup.SFTP.User,
				Password: cfg.Backup.SFTP.Password, PrivateKey: cfg.Backup.SFTP.PrivateKey,
				BasePath: cfg.Backup.SFTP.BasePath, HostKey: cfg.Backup.SFTP.HostKey,
			}))
			log.Info("backup sftp target configured", "host", cfg.Backup.SFTP.Host)
		}
		// rclone target: any of rclone's cloud backends (GDrive/Dropbox/OneDrive…)
		// via an operator-configured remote — no OAuth code or provider SDK in npd.
		if t := backuppkg.NewRcloneTarget(backuppkg.RcloneConfig{
			Bin: cfg.Backup.Rclone.Bin, Config: cfg.Backup.Rclone.Config, Remote: cfg.Backup.Rclone.Remote,
		}); t != nil {
			backupSvc = backupSvc.WithTarget(backuppkg.TargetRclone, t)
			log.Info("backup rclone target configured", "remote", cfg.Backup.Rclone.Remote)
		}
		// Panel self-backup: the panel's own database on the same pipeline,
		// sealed with the same derived key. Restore is out-of-band by design
		// (`npd decrypt` + docs/22 §7).
		if cfg.Backup.Panel.Enabled {
			backupSvc = backupSvc.WithPanel(backupStore, panelSnapshotter(db, cfg.Database, gw), backuppkg.PanelPolicy{
				Target: cfg.Backup.Panel.Target, IntervalHours: cfg.Backup.Panel.IntervalHours, Keep: cfg.Backup.Panel.Keep,
			})
			if backupSvc.PanelAvailable() {
				safe.Go(ctx, log, "backup-panel-scheduler", func(ctx context.Context) { backupSvc.RunPanelScheduler(ctx, log) })
				log.Info("panel self-backup enabled",
					"interval_hours", cfg.Backup.Panel.IntervalHours, "target", cfg.Backup.Panel.Target)
			}
		}
		// Panel self-update (docs/26). Wired after backups so it can take a
		// database snapshot before applying — migrations are the one part of an
		// update that copying the old binaries back cannot undo.
		updateSvc = update.NewService(
			repository.NewPanelUpdateStore(db), gw,
			update.Config{Channel: cfg.Update.Channel, BaseURL: cfg.Update.BaseURL, PubKey: cfg.Update.PubKey},
			version, updateDataDir(cfg), log,
		).WithSnapshotter(panelBackupSnapshotter{svc: backupSvc})
		// An update destroys the process that started it, so nobody is left to
		// record how it went. Settle whatever was in flight from the installer's
		// result file — or, failing that, from the version we came back as.
		if err := updateSvc.Reconcile(ctx); err != nil {
			log.Warn("self-update: could not reconcile the last attempt", "err", err)
		}

		if backupSvc.Available() {
			safe.Go(ctx, log, "backup-scheduler", func(ctx context.Context) {
				backupSvc.RunScheduler(ctx, func(ctx context.Context, id int64) (string, bool) {
					rec, err := siteStore.GetByID(ctx, id)
					if err != nil {
						return "", false
					}
					return rec.UID, true
				}, log)
			})
			log.Info("backup scheduler enabled")
		}
		// Mail: Postfix + Dovecot driven by rendered flat maps through the
		// broker. The MTAs never read this database — mail keeps flowing when
		// the panel is down. DKIM keys are sealed with the panel cipher; DNS
		// records auto-wire into panel-managed zones when the DNS module runs.
		mailSvc = mailpkg.NewService(repository.NewMailStore(db), gw).
			WithSecrets(cipher).
			WithResolver(cfg.Mail.Resolver)
		if dnsSvc != nil {
			mailSvc = mailSvc.WithDNS(mailDNSAdapter{svc: dnsSvc})
		}
		// Passwordless webmail sign-on: mint one-time Dovecot master credentials
		// that hand off to Roundcube. Enabled only when webmail has a hostname.
		if cfg.Webmail.Hostname != "" {
			mailSvc = mailSvc.WithWebmailSSO(
				"https://"+cfg.Webmail.Hostname+"/",
				repository.NewWebmailSSOStore(db))
		}
		// Firewall: nftables with a snapshot-and-revert safety net. The guard
		// runs in-process (npd is local to the box, so it can always fire the
		// revert even when the change locked out remote access) and recovers a
		// pending change across a restart from the persisted deadline.
		firewallSvc = security.NewFirewall(repository.NewFirewallStore(db), gw)
		if cfg.Security.FirewallWindowSec > 0 {
			firewallSvc = firewallSvc.WithWindow(time.Duration(cfg.Security.FirewallWindowSec) * time.Second)
		}
		firewallSvc = firewallSvc.WithGeoSource(cfg.Security.GeoDBURLv4, cfg.Security.GeoDBURLv6)
		if firewallSvc.Available() {
			safe.Go(ctx, log, "firewall-guard", func(ctx context.Context) { firewallSvc.RunGuard(ctx, log) })
			log.Info("firewall guard enabled")
		}
		// Malware scanning (ClamAV) with quarantine, over the site tree.
		malwareSvc = security.NewMalware(repository.NewMalwareStore(db), gw, malwareSiteAdapter{repo: siteStore}).
			WithMaldet(cfg.Security.MaldetPath, cfg.Security.MaldetSHA256)
		// Fail2Ban surfacing (read-only view + manual ban/unban through its client).
		fail2banSvc = security.NewFail2Ban(gw)
		// SSH hardening: a panel-owned sshd drop-in, sshd -t tested, reloaded.
		sshSvc = security.NewSSH(gw)
		// Automatic security updates: a panel-owned unattended-upgrades drop-in.
		updatesSvc = security.NewUpdates(gw)
		// File-integrity monitoring (AIDE): baseline + tamper detection.
		fimSvc = security.NewFIM(gw)
		// Host audit scanners (rkhunter, lynis).
		auditScanSvc = security.NewAudit(gw)
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
		if brokerConn != nil {
			terminalSvc = terminal.NewService(terminalSiteAdapter{repo: siteStore}, brokerConn)
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
					"fix", "create the directory and make it writable by the npd user, or set terminal.recording.dir to \"\" to silence this")
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

		// ZeroSSL: a second ACME CA, enabled when the operator supplies EAB
		// credentials (KID from config, HMAC key from the secret env).
		if cfg.SSL.ZeroSSLEABKID != "" && cfg.SSL.ZeroSSLEABHMAC != "" {
			dir := cfg.SSL.ZeroSSLDirectory
			if dir == "" {
				dir = ssl.ZeroSSLDirectory
			}
			if zs, err := ssl.NewACMEIssuer(dir, cfg.SSL.Email, cfg.SSL.ZeroSSLEABKID, cfg.SSL.ZeroSSLEABHMAC); err != nil {
				log.Warn("could not initialize ZeroSSL", "err", err)
			} else {
				sslSvc = sslSvc.WithIssuer(ssl.ProviderZeroSSL, zs)
				log.Info("ZeroSSL enabled (EAB)")
			}
		}

		// Mail TLS: the mail host presents one certificate (its own FQDN) on
		// submission/587, imaps/993 and smtps/465. Wire the SSL module in as the
		// cert provider so a real Let's Encrypt cert is used when the operator has
		// issued one, and a self-signed fallback otherwise — TLS out of the box.
		if mailSvc != nil && cfg.Mail.Hostname != "" {
			mailSvc = mailSvc.WithTLS(cfg.Mail.Hostname, mailCertAdapter{ssl: sslSvc})
			log.Info("mail TLS configured", "hostname", cfg.Mail.Hostname)
		}

		// Webmail: Roundcube served by the panel's own OLS/PHP against the local
		// Dovecot/Postfix, as a system vhost on the configured webmail hostname.
		if cfg.Webmail.Hostname != "" && siteSvc != nil {
			webmailSvc = webmail.NewService(gw, cfg.Webmail.Hostname, cfg.Webmail.PHPVersion).
				WithReloader(siteSvc)
			siteSvc.WithSystemVhosts(webmailSvc.SystemVhosts)
			log.Info("webmail configured", "hostname", cfg.Webmail.Hostname)
		}

		log.Info("auth ready", "session_ttl", auth.DefaultConfig().SessionTTL.String())
	}

	// The module registry. In-core features register the capabilities they
	// provide — but only the ones actually wired (their datastore is present) —
	// so the set the UI gates on reflects what this npd can really do, not what
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
		renewer := ssl.NewRenewer(sslSvc, log)
		safe.Go(ctx, log, "cert-renewer", func(ctx context.Context) { renewer.Run(ctx) })
		log.Info("certificate renewer enabled",
			"interval", ssl.DefaultRenewInterval.String(), "window", ssl.DefaultRenewWindow.String())
	}

	// Drops the throwaway accounts minted for Adminer hand-offs once they expire.
	// It also sweeps on startup, cleaning up after a restart mid-session.
	if dbSvc != nil && cfg.Database.AdminerURL != "" {
		if recordings != nil {
			// Retention is not optional housekeeping: it is the half of the policy
			// that says the panel stops holding a transcript of someone's work.
			safe.Go(ctx, log, "recordings-purger", func(ctx context.Context) { recordings.RunPurger(ctx, log) })
		}
		safe.Go(ctx, log, "db-sso-sweeper", func(ctx context.Context) { dbSvc.RunSSOSweeper(ctx, log) })
		log.Info("database sign-on sweeper enabled", "ttl", database.SSOTTL.String())
	}
	// Prune expired one-time webmail SSO master users the same way.
	if mailSvc != nil && mailSvc.WebmailSSOAvailable() {
		safe.Go(ctx, log, "webmail-sso-sweeper", func(ctx context.Context) { mailSvc.RunWebmailSSOSweeper(ctx, log) })
		log.Info("webmail sign-on sweeper enabled", "ttl", mailpkg.WebmailSSOTTL.String())
	}
	// Drain the outbound webhook delivery queue (sign + POST + retry/backoff).
	if webhookStore != nil {
		dispatcher := webhook.NewDispatcher(webhookStore, log)
		safe.Go(ctx, log, "webhook-dispatcher", func(ctx context.Context) { dispatcher.Run(ctx) })
		log.Info("webhook dispatcher enabled")
	}

	// Node metrics. Needs no datastore or broker — /proc is world-readable — so
	// the monitor exists whenever npd does. Per-site and service metrics are wired
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
		// NP_MONITOR_PERSIST_SEC so a firing can be proven without a real minute.
		persistEvery := time.Duration(0)
		if v := os.Getenv("NP_MONITOR_PERSIST_SEC"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				persistEvery = time.Duration(n) * time.Second
			}
		}
		safe.Go(ctx, log, "monitor-persister", func(ctx context.Context) { monitorSvc.RunPersister(ctx, persistEvery, log) })
		safe.Go(ctx, log, "monitor-rollup", func(ctx context.Context) { monitorSvc.RunRollup(ctx, log) })
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
		hub := wsHub
		safe.Go(ctx, log, "ws-hub", func(ctx context.Context) { hub.Run(ctx) })
		// Push node samples to subscribed dashboards, sampling only while watched.
		safe.Go(ctx, log, "monitor-sampler", func(ctx context.Context) { monitorSvc.RunSampler(ctx, hub, monitor.DefaultInterval, log) })
		log.Info("realtime hub enabled", "redis_bridge", cacheWiring.RedisClient != nil)
	}

	handler := httpapi.NewRouter(httpapi.Deps{
		Ctx:         ctx,
		Config:      cfg,
		Logger:      log,
		Version:     version,
		StartedAt:   time.Now(),
		DB:          dbHealth,
		Redis:       redisHealth,
		Broker:      brokerHealth,
		Auth:        authSvc,
		Audit:       auditSvc,
		Users:       userDir,
		UserMgmt:    userMgmt,
		Tenancy:     tenancyResolver,
		Webhooks:    webhookSvc,
		Marketplace: marketplaceSvc,
		Setup:       setupSvc,
		Update:      updateSvc,
		Keyring:     keyringSvc,
		Sites:       siteSvc,
		PHP:         phpSvc,
		Databases:   dbSvc,
		SSL:         sslSvc,
		DNS:         dnsSvc,
		Domains:     domainSvc,
		Git:         gitSvc,
		Docker:      dockerSvc,
		Apps:        appsSvc,
		Files:       filesSvc,
		Terminal:    terminalSvc,
		Recordings:  recordings,
		Runtime:     runtimeSvc,
		Cron:        cronSvc,
		Backups:     backupSvc,
		Mail:        mailSvc,
		Webmail:     webmailSvc,
		Firewall:    firewallSvc,
		Malware:     malwareSvc,
		Fail2Ban:    fail2banSvc,
		SSH:         sshSvc,
		Updates:     updatesSvc,
		FIM:         fimSvc,
		AuditScan:   auditScanSvc,
		Monitor:     monitorSvc,
		Jobs:        jobs,
		Registry:    reg,
		WS:          wsHub,
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

	// systemd integration (no-op when not run under systemd): report readiness
	// for the Type=notify unit, then pet the watchdog on its interval so a hung
	// npd — not just a crashed one — is killed and restarted. The pinger is
	// supervised so it can never itself crash the process.
	sd := systemd.New()
	defer func() { _ = sd.Close() }()
	if err := sd.Ready(); err != nil {
		a.log.Warn("systemd readiness notification failed", "err", err)
	}
	if iv := systemd.WatchdogInterval(); iv > 0 && sd.Enabled() {
		a.log.Info("systemd watchdog enabled", "ping_interval", iv.String())
		safe.Go(ctx, a.log, "systemd-watchdog", func(ctx context.Context) {
			t := time.NewTicker(iv)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					_ = sd.Watchdog()
				}
			}
		})
	}

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

// ensureTempDomainWildcard puts `*.<panel domain> A <panel ip>` in place when
// the panel's own base domain happens to be a zone this installation hosts.
//
// Temporary site addresses each get a fresh label, so a wildcard is the only
// record that can cover them — and one that is merely *documented* is one that
// is never added, leaving every temporary address dead. Creating it here, at
// the moment the operator names the domain, is the only point where we know
// both the base and the address.
//
// The panel never infers its own IP, so an operator who left that blank (or
// whose DNS lives elsewhere) simply gets the record logged for them to add;
// EnsureRecord reports false with no error when no managed zone covers the
// name, which is exactly that case rather than a failure.
func ensureTempDomainWildcard(ctx context.Context, dnsSvc *dns.Service, sel setup.Selection, log *slog.Logger) {
	if dnsSvc == nil || sel.PanelDomain == "" || sel.PanelIPv4 == "" {
		return
	}
	wildcard := setup.WildcardFor(sel.PanelDomain)
	ok, err := dnsSvc.EnsureRecord(ctx, wildcard, "A", sel.PanelIPv4, 0, 3600, false)
	switch {
	case err != nil:
		log.Warn("setup: could not create the temporary-address wildcard",
			"record", wildcard, "ip", sel.PanelIPv4, "err", err)
	case ok:
		log.Info("setup: temporary-address wildcard in place", "record", wildcard, "ip", sel.PanelIPv4)
	default:
		log.Info("setup: panel domain is not a zone hosted here — add this record at your DNS provider",
			"record", wildcard, "type", "A", "value", sel.PanelIPv4)
	}
}

// updateDataDir is where self-update stages releases and leaves its result
// file. It is derived from the datastore path rather than configured
// separately: npd's systemd unit grants ReadWritePaths on exactly that
// directory, so anywhere else would need a policy change to write to, and a
// second setting to keep in step with the first.
func updateDataDir(cfg config.Config) string {
	if dsn := strings.TrimSpace(cfg.Database.DSN); dsn != "" && cfg.Database.Driver == "sqlite" {
		if dir := filepath.Dir(dsn); dir != "" && dir != "." {
			return dir
		}
	}
	return "/var/lib/nexpanel"
}

// panelBackupSnapshotter adapts the backup service to the narrow snapshotter
// the updater needs. The updater declares its own two-method interface rather
// than importing backup's types, so the two modules stay independent — this is
// the seam where they meet.
type panelBackupSnapshotter struct{ svc *backuppkg.Service }

func (p panelBackupSnapshotter) PanelAvailable() bool {
	return p.svc != nil && p.svc.PanelAvailable()
}

func (p panelBackupSnapshotter) CreatePanelBackup(ctx context.Context) (*update.BackupRef, error) {
	b, err := p.svc.CreatePanelBackup(ctx)
	if err != nil {
		return nil, err
	}
	return &update.BackupRef{UID: b.UID}, nil
}
