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

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/site"
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
	DB        HealthChecker // nil when no datastore is configured
	Redis     HealthChecker // nil when Redis is disabled
	Broker    HealthChecker // nil when the broker is not configured
	Auth      *auth.Service // nil when no datastore is configured
	Users     UserDirectory // nil when no datastore is configured
	Sites     *site.Service // nil when no datastore is configured
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

		// Auth-dependent routes are mounted only when a datastore (and thus the
		// auth service) is available.
		if d.Auth != nil {
			r.Group(func(r chi.Router) {
				r.Use(authenticate(d.Auth)) // attach principal if present

				r.Get("/auth/status", statusHandler(d))
				r.Post("/auth/bootstrap", bootstrapHandler(d))
				r.Post("/auth/login", loginHandler(d))
				r.With(requireAuth).Post("/auth/logout", logoutHandler(d))
				r.With(requireAuth).Get("/auth/me", meHandler)

				if d.Users != nil {
					r.With(requirePermission("user.read")).Get("/users", listUsersHandler(d))
				}
				if d.Sites != nil {
					r.With(requirePermission("site.read")).Get("/sites", listSitesHandler(d))
					r.With(requirePermission("site.write")).Post("/sites", createSiteHandler(d))
					r.With(requirePermission("site.read")).Get("/sites/{uid}", getSiteHandler(d))
					r.With(requirePermission("site.write")).Delete("/sites/{uid}", deleteSiteHandler(d))
					r.With(requirePermission("site.read")).Get("/sites/{uid}/php", getSitePHPHandler(d))
					r.With(requirePermission("site.write")).Put("/sites/{uid}/php", setSitePHPHandler(d))
				}
			})
		}
		// Future: /dns, /ssl, ... mounted here.
	})

	// Embedded SPA (served for GET/HEAD on all non-API routes; falls back to a
	// placeholder when no frontend build is embedded). Registering only GET/HEAD
	// preserves 405 semantics for wrong-method requests to real routes.
	distFS, hasSPA := web.FS()
	spa := spaHandler(distFS, hasSPA)
	r.Get("/*", spa)
	r.Head("/*", spa)

	return r
}
