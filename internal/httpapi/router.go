// Package httpapi builds hpd's HTTP edge: the Chi router, the standard
// middleware chain, the JSON response/error contract, and the baseline
// endpoints. Business handlers are added per bounded context (docs/04).
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/thisisnkp/heropanel/internal/apps"
	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/backup"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/cron"
	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/internal/docker"
	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/internal/files"
	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/monitor"
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/registry"
	"github.com/thisisnkp/heropanel/internal/runtime"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/ssl"
	"github.com/thisisnkp/heropanel/internal/terminal"
	"github.com/thisisnkp/heropanel/internal/ws"
	"github.com/thisisnkp/heropanel/web"
)

// HealthChecker is anything whose health can be probed (e.g. the database).
// The router depends on this narrow interface, not on the repository package.
type HealthChecker interface {
	Health(ctx context.Context) error
}

// Deps are everything the router needs. Services are added here as they land.
type Deps struct {
	Ctx       context.Context // lifecycle context (cancels background helpers)
	Config    config.Config
	Logger    *slog.Logger
	Version   string
	StartedAt time.Time
	DB        HealthChecker     // nil when no datastore is configured
	Redis     HealthChecker     // nil when Redis is disabled
	Broker    HealthChecker     // nil when the broker is not configured
	Auth      *auth.Service     // nil when no datastore is configured
	Audit     *audit.Service    // nil when no datastore is configured
	Users     UserDirectory     // nil when no datastore is configured
	Sites     *site.Service     // nil when no datastore is configured
	PHP       *php.Service      // nil when no datastore is configured
	Databases *database.Service // nil when no datastore is configured
	SSL       *ssl.Service      // nil when no datastore is configured
	DNS       *dns.Service      // nil when no datastore is configured
	Domains   *domain.Service   // nil when no datastore is configured
	Git       *git.Service      // nil when no datastore is configured
	Files     *files.Service    // nil when no datastore is configured
	Terminal  *terminal.Service // nil when no datastore is configured
	// Recordings stores terminal session transcripts. It may be nil, and its
	// Enabled() reports false when no recordings directory is configured — a
	// terminal still works, it is simply not recorded.
	Recordings *terminal.RecordingStore
	// Docker manages containers on the host. It is nil when the broker is not
	// configured — every operation is a privileged capability, so without a
	// broker there is nothing this module could do.
	Docker *docker.Service
	// Apps is the one-click catalog. It rides on Docker, so it is nil in exactly
	// the same conditions Docker is.
	Apps    *apps.Service
	Runtime *runtime.Service // nil when no datastore is configured
	Cron    *cron.Service    // nil when no datastore is configured
	Backups *backup.Service  // nil when no datastore is configured
	// Monitor samples node health for the dashboard. It needs no datastore or
	// broker (node metrics come from world-readable /proc), so it is present
	// whenever hpd is.
	Monitor  *monitor.Service
	Jobs     *job.Dispatcher    // nil when the async job queue is disabled (no Redis)
	WS       *ws.Hub            // nil when the realtime hub is disabled (no Redis)
	Registry *registry.Registry // module capability set; never nil (may be empty)
}

// NewRouter assembles the middleware chain and routes into an http.Handler.
func NewRouter(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Ctx == nil {
		d.Ctx = context.Background()
	}

	r := chi.NewRouter()

	// Middleware chain (order matters — see docs/01 §3.1). Authn/Authz slot in
	// after AccessLog once the auth layer lands.
	r.Use(middleware.RequestID)
	r.Use(exposeRequestID())
	r.Use(middleware.RealIP)
	r.Use(recoverer(d.Logger))
	r.Use(securityHeaders(d.Config.Server.TLS.Enabled))
	r.Use(cors(d.Config.Security.CORS))
	if d.Config.Security.RateLimit.Enabled {
		r.Use(newRateLimiter(d.Ctx, d.Config.Security.RateLimit).middleware())
	}
	r.Use(accessLog(d.Logger))
	r.Use(bodyLimit(d.Config.Security.BodyLimitBytes))

	r.NotFound(notFoundHandler)
	r.MethodNotAllowed(methodNotAllowedHandler)

	// Infra probes (no envelope, no auth; bind-scoped in production).
	r.Get("/healthz", healthHandler)
	r.Get("/readyz", readyHandler(d))

	// Versioned API surface.
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/system/info", systemInfoHandler(d))
		// The OpenAPI 3.1 description of this instance's surface. Unauthenticated:
		// it names no secrets and a client needs it to learn how to authenticate.
		// Built by walking the live routing tree (openapi.go), so it cannot drift.
		r.Get("/openapi.json", openapiHandler(d))

		// Auth-dependent routes are mounted only when a datastore (and thus the
		// auth service) is available. When it is not, the panel cannot sign anyone
		// in — so the three routes the login screen actually calls are still
		// mounted, answering with the real reason. Without them every auth route
		// 404s and the operator is told "the requested resource was not found"
		// when the actual problem is that no database is configured.
		if d.Auth == nil {
			r.Get("/auth/status", unconfiguredStatusHandler())
			r.Post("/auth/login", unconfiguredAuthHandler())
			r.Post("/auth/bootstrap", unconfiguredAuthHandler())
		}
		if d.Auth != nil {
			r.Group(func(r chi.Router) {
				r.Use(authenticate(d.Auth)) // attach principal if present
				// The auditor sits above CSRF so that a rejected mutation is
				// still recorded: "someone tried and was refused" is precisely
				// the entry an operator goes looking for. Below CSRF it would
				// never run, because CSRF short-circuits the chain.
				r.Use(auditor(d.Audit, d.Logger))
				r.Use(csrf(d.Config.Security.CSRF.Enabled)) // double-submit CSRF (opt-in)

				r.Get("/auth/status", statusHandler(d))
				r.Post("/auth/bootstrap", bootstrapHandler(d))
				r.Post("/auth/login", loginHandler(d))
				r.Post("/auth/mfa", mfaCompleteHandler(d)) // completes an MFA login (pre-session)
				r.With(requireAuth).Post("/auth/logout", logoutHandler(d))
				r.With(requireAuth).Get("/auth/me", meHandler)
				r.With(requireAuth).Post("/auth/mfa/setup", mfaSetupHandler(d))
				r.With(requireAuth).Post("/auth/mfa/enable", mfaEnableHandler(d))
				r.With(requireAuth).Post("/auth/mfa/disable", mfaDisableHandler(d))

				// API keys (scoped programmatic access).
				r.With(requireAuth).Get("/account/api-keys", listAPIKeysHandler(d))
				r.With(requireAuth).Post("/account/api-keys", createAPIKeyHandler(d))
				r.With(requireAuth).Delete("/account/api-keys/{uid}", revokeAPIKeyHandler(d))

				if d.Users != nil {
					r.With(requirePermission("user.read")).Get("/users", listUsersHandler(d))
				}
				// The module/capability set. The UI reads this at login to decide
				// which features to render (docs/06 §6); a feature whose module is
				// absent greys out rather than 404-ing on click.
				r.With(requireAuth).Get("/capabilities", capabilitiesHandler(d))
				r.With(requireAuth).Get("/modules", modulesHandler(d))
				if d.Audit != nil {
					r.With(requirePermission("audit.read")).Get("/audit", listAuditHandler(d))
					r.With(requirePermission("audit.read")).Get("/audit/verify", verifyAuditHandler(d))
				}
				if d.PHP != nil {
					// Server-scope, not site-scope: an extension belongs to a PHP
					// version and toggling it restarts FPM for every site on it.
					// Gating this behind site.write would let a tenant with one
					// site restart everyone else's.
					r.With(requirePermission("system.read")).Get("/php/extensions", listPHPExtensionsHandler(d))
					r.With(requirePermission("system.write")).Post("/php/extensions", setPHPExtensionHandler(d))
				}
				if d.Databases != nil {
					r.With(requirePermission("database.read")).Get("/databases", listDatabasesHandler(d))
					r.With(requirePermission("database.write")).Post("/databases", createDatabaseHandler(d))
					r.With(requirePermission("database.write")).Delete("/databases/{uid}", deleteDatabaseHandler(d))
					r.With(requirePermission("database.write")).Post("/databases/{uid}/grant", grantDatabaseHandler(d))
					r.With(requirePermission("database.write")).Post("/databases/{uid}/revoke", revokeDatabaseHandler(d))
					r.With(requirePermission("database.read")).Get("/databases/{uid}/size", databaseSizeHandler(d))
					// An export is a full copy of the data leaving the server, so it
					// takes write, not read.
					r.With(requirePermission("database.write")).Get("/databases/{uid}/export", exportDatabaseHandler(d))
					r.With(requirePermission("database.write")).Post("/databases/{uid}/import", importDatabaseHandler(d))
					// The hand-off mints a live credential, so it is write-gated
					// even though it only reads data.
					r.With(requirePermission("database.write")).Post("/databases/{uid}/adminer-sso", adminerSSOHandler(d))
					r.With(requirePermission("database.read")).Get("/database-users", listDBUsersHandler(d))
					r.With(requirePermission("database.write")).Post("/database-users", createDBUserHandler(d))
					r.With(requirePermission("database.write")).Delete("/database-users/{uid}", deleteDBUserHandler(d))
				}
				if d.SSL != nil {
					r.With(requirePermission("ssl.read")).Get("/ssl/certificates", listCertsHandler(d))
					r.With(requirePermission("ssl.write")).Post("/ssl/self-signed", issueSelfSignedHandler(d))
					r.With(requirePermission("ssl.write")).Post("/ssl/upload", uploadCertHandler(d))
					r.With(requirePermission("ssl.write")).Post("/ssl/issue", issueCertHandler(d))
				}
				if d.Sites != nil {
					r.With(requirePermission("site.read")).Get("/sites", listSitesHandler(d))
					r.With(requirePermission("site.write")).Post("/sites", createSiteHandler(d))
					r.With(requirePermission("site.read")).Get("/sites/{uid}", getSiteHandler(d))
					r.With(requirePermission("site.write")).Delete("/sites/{uid}", deleteSiteHandler(d))
					r.With(requirePermission("site.read")).Get("/sites/{uid}/php", getSitePHPHandler(d))
					r.With(requirePermission("site.write")).Put("/sites/{uid}/php", setSitePHPHandler(d))
					r.With(requirePermission("site.read")).Get("/sites/{uid}/limits", getSiteLimitsHandler(d))
					r.With(requirePermission("site.write")).Put("/sites/{uid}/limits", setSiteLimitsHandler(d))
					r.With(requirePermission("site.read")).Get("/sites/{uid}/logs", siteLogsHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/suspend", suspendSiteHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/resume", resumeSiteHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/clone", cloneSiteHandler(d))
				}
				if d.DNS != nil {
					r.With(requirePermission("dns.read")).Get("/dns/zones", listZonesHandler(d))
					r.With(requirePermission("dns.write")).Post("/dns/zones", createZoneHandler(d))
					r.With(requirePermission("dns.read")).Get("/dns/zones/{uid}", getZoneHandler(d))
					r.With(requirePermission("dns.write")).Delete("/dns/zones/{uid}", deleteZoneHandler(d))
					r.With(requirePermission("dns.read")).Get("/dns/zones/{uid}/records", listRecordsHandler(d))
					r.With(requirePermission("dns.write")).Post("/dns/zones/{uid}/records", createRecordHandler(d))
					r.With(requirePermission("dns.write")).Delete("/dns/records/{uid}", deleteRecordHandler(d))
				}
				if d.Git != nil {
					r.With(requirePermission("git.read")).Get("/sites/{uid}/git", getSiteGitHandler(d))
					r.With(requirePermission("git.write")).Put("/sites/{uid}/git", setSiteGitHandler(d))
					r.With(requirePermission("git.read")).Get("/sites/{uid}/git/deployments", listSiteDeploymentsHandler(d))
					r.With(requirePermission("git.write")).Post("/sites/{uid}/git/deploy", deploySiteHandler(d))
					r.With(requirePermission("git.write")).Post("/sites/{uid}/git/rollback/{dep}", rollbackSiteHandler(d))
				}
				if d.Files != nil {
					// The File Manager is baremetal-only (enforced in the service).
					// Reads (browse, download) take file.read; every mutation takes
					// file.write. A download hands a file's bytes to the caller, so it
					// is read-gated but force-audited in the handler.
					r.With(requirePermission("file.read")).Get("/sites/{uid}/files", listFilesHandler(d))
					r.With(requirePermission("file.read")).Get("/sites/{uid}/files/content", readFileHandler(d))
					r.With(requirePermission("file.read")).Get("/sites/{uid}/files/archive", archiveFileHandler(d))
					r.With(requirePermission("file.write")).Put("/sites/{uid}/files/content", writeFileHandler(d))
					r.With(requirePermission("file.write")).Delete("/sites/{uid}/files", deleteFileHandler(d))
					r.With(requirePermission("file.write")).Post("/sites/{uid}/files/mkdir", mkdirFileHandler(d))
					r.With(requirePermission("file.write")).Post("/sites/{uid}/files/rename", renameFileHandler(d))
					r.With(requirePermission("file.write")).Post("/sites/{uid}/files/copy", copyFileHandler(d))
					r.With(requirePermission("file.write")).Post("/sites/{uid}/files/move", moveFileHandler(d))
					r.With(requirePermission("file.write")).Post("/sites/{uid}/files/chmod", chmodFileHandler(d))
					r.With(requirePermission("file.write")).Post("/sites/{uid}/files/extract", extractFileHandler(d))
					r.With(requirePermission("file.write")).Post("/sites/{uid}/files/compress", compressFileHandler(d))
					r.With(requirePermission("file.write")).Post("/sites/{uid}/files/chown", chownFileHandler(d))
					r.With(requirePermission("file.read")).Get("/sites/{uid}/files/search", searchFilesHandler(d))
				}
				if d.Terminal != nil && d.Terminal.Available() {
					// An interactive shell as the site's Linux user. Its own
					// permission, not file.write: being able to edit a file is a
					// much smaller grant than being able to run arbitrary commands
					// as that user. Force-audited in the handler.
					r.With(requirePermission("terminal.use")).Get("/sites/{uid}/terminal", terminalHandler(d))
				}
				if d.Recordings != nil {
					// Recordings are a transcript of privileged work, so they carry
					// their own permission rather than riding on terminal.use: being
					// allowed to open your own shell is a much smaller grant than being
					// allowed to read what everyone else typed in theirs.
					r.With(requirePermission("terminal.recordings.read")).
						Get("/sites/{uid}/terminal/recordings", listRecordingsHandler(d))
					r.With(requirePermission("terminal.recordings.read")).
						Get("/terminal/recordings", listRecordingsHandler(d))
					r.With(requirePermission("terminal.recordings.read")).
						Get("/terminal/recordings/{rid}", getRecordingHandler(d))
					r.With(requirePermission("terminal.recordings.read")).
						Get("/terminal/recordings/{rid}/cast", downloadRecordingHandler(d))
					r.With(requirePermission("terminal.recordings.delete")).
						Delete("/terminal/recordings/{rid}", deleteRecordingHandler(d))
				}
				if d.Docker != nil {
					// Docker is host-wide, not site-scoped, so it gets its own two
					// permissions rather than riding on site.*: stopping the container
					// that serves a site is a different act from editing the site, and
					// the listing covers containers no site owns.
					//
					// Ownership is enforced in the broker, which refuses any container
					// the panel did not create — deliberately not re-checked here, so a
					// future route cannot forget it.
					r.With(requirePermission("docker.read")).Get("/docker/info", dockerInfoHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/containers", listContainersHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/containers/{id}", inspectContainerHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/containers/{id}/logs", containerLogsHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/containers/{id}/stats", containerStatsHandler(d))
					// Host-wide sample. Same handler: with no {id} in the route the
					// param is empty, which the service reads as "every container" —
					// so the two views cannot drift in what they measure.
					r.With(requirePermission("docker.read")).Get("/docker/stats", containerStatsHandler(d))
					r.With(requirePermission("docker.write")).Post("/docker/containers/{id}/start", containerActionHandler(d, "start"))
					r.With(requirePermission("docker.write")).Post("/docker/containers/{id}/stop", containerActionHandler(d, "stop"))
					r.With(requirePermission("docker.write")).Post("/docker/containers/{id}/restart", containerActionHandler(d, "restart"))
					r.With(requirePermission("docker.write")).Delete("/docker/containers/{id}", containerActionHandler(d, "remove"))
					r.With(requirePermission("docker.read")).Get("/docker/images", listImagesHandler(d))
					r.With(requirePermission("docker.write")).Post("/docker/images/pull", pullImageHandler(d))
					r.With(requirePermission("docker.write")).Post("/docker/images/prune", pruneImagesHandler(d))
					r.With(requirePermission("docker.write")).Delete("/docker/images/{ref}", removeImageHandler(d))
					r.With(requirePermission("docker.write")).Post("/docker/containers", createContainerHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/volumes", listVolumesHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/volumes/{name}", inspectVolumeHandler(d))
					r.With(requirePermission("docker.write")).Post("/docker/volumes", createVolumeHandler(d))
					r.With(requirePermission("docker.write")).Delete("/docker/volumes/{name}", removeVolumeHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/networks", listNetworksHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/networks/{name}", inspectNetworkHandler(d))
					r.With(requirePermission("docker.write")).Post("/docker/networks", createNetworkHandler(d))
					if d.Docker.ExecEnabled() {
						// A shell inside a container. Mounted only when the broker can
						// stream, so the UI never offers a button that cannot work.
						r.With(requirePermission("docker.write")).Get("/docker/containers/{id}/exec", containerExecHandler(d))
					}
					if d.Docker.LogStreamEnabled() {
						// Live log follow. Same streaming precondition as exec, but a
						// read: gated by docker.read, force-audited in the handler.
						r.With(requirePermission("docker.read")).Get("/docker/containers/{id}/logs/stream", containerLogsStreamHandler(d))
					}
					// Compose stacks. Same permissions as single containers: a stack
					// is containers.
					r.With(requirePermission("docker.read")).Get("/docker/compose/{project}", composeStatusHandler(d))
					r.With(requirePermission("docker.read")).Get("/docker/compose/{project}/logs", composeLogsHandler(d))
					r.With(requirePermission("docker.write")).Post("/docker/compose", composeUpHandler(d))
					r.With(requirePermission("docker.write")).Delete("/docker/compose/{project}", composeDownHandler(d))
					r.With(requirePermission("docker.write")).Delete("/docker/networks/{name}", removeNetworkHandler(d))

					// One-click apps. Same permissions as the containers they deploy:
					// browsing and status are docker.read; deploying and removing are
					// docker.write. No separate "apps" permission — an app is a
					// container stack, not a new kind of privilege.
					if d.Apps != nil {
						r.With(requirePermission("docker.read")).Get("/apps/templates", listAppTemplatesHandler(d))
						r.With(requirePermission("docker.write")).Post("/apps", deployAppHandler(d))
						r.With(requirePermission("docker.read")).Get("/apps/{project}", appStatusHandler(d))
						r.With(requirePermission("docker.read")).Get("/apps/{project}/logs", appLogsHandler(d))
						r.With(requirePermission("docker.write")).Delete("/apps/{project}", removeAppHandler(d))
						// Exposing an app to a domain creates a real proxy site, so it is a
						// site.write operation, and reading its exposure is docker.read.
						r.With(requirePermission("docker.read")).Get("/apps/{project}/exposure", appExposureHandler(d))
						r.With(requirePermission("site.write")).Post("/apps/{project}/expose", exposeAppHandler(d))
						r.With(requirePermission("site.write")).Delete("/apps/{project}/expose", unexposeAppHandler(d))
					}
				}
				if d.Monitor != nil {
					// Host-wide metrics, like Docker. One-shot read for the initial
					// paint; the live dashboard rides the `monitor:*` WS channels,
					// which the same permission gates.
					r.With(requirePermission("monitor.read")).Get("/monitor/node", monitorNodeHandler(d))
					r.With(requirePermission("monitor.read")).Get("/monitor/sites", monitorSitesHandler(d))
					r.With(requirePermission("monitor.read")).Get("/monitor/services", monitorServicesHandler(d))
					if d.Monitor.HistoryEnabled() {
						r.With(requirePermission("monitor.read")).Get("/monitor/history", monitorHistoryHandler(d))
					}
					if d.Monitor.AlertsEnabled() {
						// Reading rules and events is monitor.read; changing them is
						// monitor.write — configuring what pages an operator is a heavier
						// grant than watching the graphs.
						r.With(requirePermission("monitor.read")).Get("/monitor/alerts/rules", listAlertRulesHandler(d))
						r.With(requirePermission("monitor.read")).Get("/monitor/alerts/events", listAlertEventsHandler(d))
						r.With(requirePermission("monitor.write")).Post("/monitor/alerts/rules", createAlertRuleHandler(d))
						r.With(requirePermission("monitor.write")).Put("/monitor/alerts/rules/{uid}", toggleAlertRuleHandler(d))
						r.With(requirePermission("monitor.write")).Delete("/monitor/alerts/rules/{uid}", deleteAlertRuleHandler(d))
					}
				}
				if d.Domains != nil {
					r.With(requirePermission("site.read")).Get("/sites/{uid}/domains", listDomainsHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/domains", addDomainHandler(d))
					r.With(requirePermission("site.write")).Delete("/sites/{uid}/domains/{did}", deleteDomainHandler(d))
					r.With(requirePermission("site.write")).Put("/sites/{uid}/force-https", setForceHTTPSHandler(d))
				}
				if d.Runtime != nil {
					r.With(requirePermission("site.read")).Get("/sites/{uid}/runtime", getSiteRuntimeHandler(d))
					r.With(requirePermission("site.read")).Get("/sites/{uid}/runtime/health", runtimeHealthHandler(d))
					r.With(requirePermission("site.write")).Put("/sites/{uid}/runtime", setSiteRuntimeHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/runtime/start", runtimeControlHandler(d, "start"))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/runtime/stop", runtimeControlHandler(d, "stop"))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/runtime/restart", runtimeControlHandler(d, "restart"))
				}
				if d.Cron != nil {
					// Scheduled jobs are site-scoped, so they ride on the site
					// permissions — listing/logs are site.read, mutations site.write.
					r.With(requirePermission("site.read")).Get("/sites/{uid}/cron", listCronJobsHandler(d))
					r.With(requirePermission("site.read")).Get("/sites/{uid}/cron/{jid}/logs", cronJobLogsHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/cron", createCronJobHandler(d))
					r.With(requirePermission("site.write")).Put("/sites/{uid}/cron/{jid}", toggleCronJobHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/cron/{jid}/run", runCronJobHandler(d))
					r.With(requirePermission("site.write")).Delete("/sites/{uid}/cron/{jid}", deleteCronJobHandler(d))
				}
				if d.Backups != nil {
					// Backups hold everything the site holds, so they ride on the
					// site permissions; restore provisions a NEW site (site.write).
					r.With(requirePermission("site.read")).Get("/sites/{uid}/backups", listBackupsHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/backups", createBackupHandler(d))
					r.With(requirePermission("site.write")).Put("/sites/{uid}/backups/config", setBackupConfigHandler(d))
					r.With(requirePermission("site.write")).Post("/sites/{uid}/backups/{bid}/restore", restoreBackupHandler(d))
					r.With(requirePermission("site.write")).Delete("/sites/{uid}/backups/{bid}", deleteBackupHandler(d))
					// Panel self-backup: the panel's own database, sealed. Restore
					// is out-of-band by design (`hpd decrypt` + docs/22 §7).
					r.With(requirePermission("system.read")).Get("/system/backups", listPanelBackupsHandler(d))
					r.With(requirePermission("system.write")).Post("/system/backups", createPanelBackupHandler(d))
					r.With(requirePermission("system.write")).Delete("/system/backups/{uid}", deletePanelBackupHandler(d))
				}
				if d.Jobs != nil {
					r.With(requireAuth).Get("/jobs", listJobsHandler(d))
					r.With(requireAuth).Get("/jobs/{id}", getJobHandler(d))
				}
				if d.WS != nil {
					r.With(requireAuth).Get("/ws", d.WS.Handler())
				}
			})
		}
		// Future: /dns, /ssl, ... mounted here.
	})

	// Git push webhook: unauthenticated by session, authorized by the per-source
	// secret (constant-time compare in the handler). Mounted outside the auth
	// group and before the SPA catch-all so a push can trigger a deploy.
	//
	// It carries the auditor of its own accord. Being outside the auth group
	// means it is outside that group's middleware too, and an endpoint that
	// deploys code to a site on presentation of a shared secret is the last one
	// that should be missing from the chain.
	if d.Git != nil {
		r.With(auditor(d.Audit, d.Logger)).Post("/hooks/git/{uid}", gitWebhookHandler(d))
	}

	// Interactive API docs: a small viewer that renders /api/v1/openapi.json
	// client-side. Unauthenticated like the spec it renders, and served as
	// separate same-origin assets so the strict CSP needs no exception. Mounted
	// before the SPA catch-all so these exact paths are not swallowed by it.
	r.Get("/api/docs", docsPageHandler())
	r.Get("/api/docs.css", docsAssetHandler("text/css; charset=utf-8", docsCSS))
	r.Get("/api/docs.js", docsAssetHandler("application/javascript; charset=utf-8", docsJS))

	// Embedded SPA (served for GET/HEAD on all non-API routes; falls back to a
	// placeholder when no frontend build is embedded). Registering only GET/HEAD
	// preserves 405 semantics for wrong-method requests to real routes.
	distFS, hasSPA := web.FS()
	spa := spaHandler(distFS, hasSPA)
	r.Get("/*", spa)
	r.Head("/*", spa)

	return r
}
