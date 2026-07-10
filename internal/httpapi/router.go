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

	"github.com/thisisnkp/heropanel/internal/config"
)

// Deps are everything the router needs. Services are added here as they land.
type Deps struct {
	Ctx       context.Context // lifecycle context (cancels background helpers)
	Config    config.Config
	Logger    *slog.Logger
	Version   string
	StartedAt time.Time
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
		// Future: /auth, /sites, /dns, /ssl, ... mounted here.
	})

	// SPA placeholder (replaced by the embedded React build).
	r.Get("/", rootHandler(d))

	return r
}
