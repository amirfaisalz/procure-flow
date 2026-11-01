package httpapi

import (
	"log/slog"
	"net/http"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
	"github.com/amirfaisalz/procure-flow/internal/platform/metrics"
	"github.com/amirfaisalz/procure-flow/internal/platform/ratelimit"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/middleware"
)

// Dependencies are the collaborators required to build the HTTP router.
type Dependencies struct {
	Config        config.Config
	Logger        *slog.Logger
	Procurement   *app.Service
	Health        *handler.Health
	Authenticator auth.Authenticator
	Idempotency   idempotency.Repository
	RateLimiter   *ratelimit.Limiter
	Metrics       *metrics.Registry
}

// localActor is injected when authentication is disabled. It is an admin so
// local development keeps the full workflow available without credentials.
var localActor = app.Actor{
	Subject: "local-admin",
	Name:    "Local Admin",
	Roles:   []string{app.RoleAdmin},
}

// NewRouter wires routes and middleware. Middleware is ordered so the request
// deadline covers the whole chain, a request ID and trace context exist before
// logging, and panics are recovered before metrics record the final status.
//
// Health probes, API documentation, and metrics stay public; every /api/v1
// route is authenticated. When authentication is disabled the router injects a
// local admin actor, mirroring the behaviour of the in-memory repository.
func NewRouter(deps Dependencies) http.Handler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	procurementHandler := handler.NewProcurement(deps.Procurement, logger)

	apiMux := http.NewServeMux()

	createProcurement := http.Handler(http.HandlerFunc(procurementHandler.Create))
	if deps.Config.Idempotency.Enabled && deps.Idempotency != nil {
		createProcurement = middleware.Idempotency(
			deps.Idempotency,
			deps.Config.Idempotency.TTL,
			nil,
			logger,
		)(createProcurement)
	}
	apiMux.Handle("POST /api/v1/procurements", createProcurement)

	apiMux.HandleFunc("GET /api/v1/procurements", procurementHandler.List)
	apiMux.HandleFunc("GET /api/v1/procurements/{id}", procurementHandler.Get)
	apiMux.HandleFunc("GET /api/v1/procurements/{id}/audit", procurementHandler.Audit)
	apiMux.HandleFunc("POST /api/v1/procurements/{id}/submit", procurementHandler.Submit)
	apiMux.HandleFunc("POST /api/v1/procurements/{id}/approve", procurementHandler.Approve)
	apiMux.HandleFunc("POST /api/v1/procurements/{id}/reject", procurementHandler.Reject)
	apiMux.HandleFunc("POST /api/v1/procurements/{id}/cancel", procurementHandler.Cancel)

	var apiHandler http.Handler = &fallback{mux: apiMux}

	apiMiddlewares := make([]middleware.Middleware, 0, 2)
	if deps.Config.Auth.Enabled {
		authenticator := deps.Authenticator
		if authenticator == nil {
			// Fail closed: a missing authenticator rejects every request.
			authenticator = auth.Chain{}
		}
		apiMiddlewares = append(apiMiddlewares, middleware.Authentication(authenticator, logger))
	} else {
		logger.Warn("authentication is disabled; injecting a local admin actor")
		apiMiddlewares = append(apiMiddlewares, middleware.InjectActor(localActor))
	}
	if deps.Config.RateLimit.Enabled && deps.RateLimiter != nil {
		apiMiddlewares = append(apiMiddlewares, middleware.RateLimit(deps.RateLimiter, logger))
	}
	apiHandler = middleware.Chain(apiHandler, apiMiddlewares...)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", deps.Health.Live)
	mux.HandleFunc("GET /readyz", deps.Health.Ready)
	mux.Handle("/api/v1/", apiHandler)

	if deps.Config.Metrics.Enabled {
		mux.Handle("GET /metrics", handler.NewMetrics(deps.Metrics))
	}

	if deps.Config.Docs.Enabled {
		docs := newDocsHandler()
		mux.HandleFunc("GET /docs", docs.Index)
		mux.HandleFunc("GET /openapi.yaml", docs.Spec)
	}

	var router http.Handler = &fallback{mux: mux}

	middlewares := make([]middleware.Middleware, 0, 8)
	if deps.Config.HTTP.RequestTimeout > 0 {
		// Timeout is outermost so every middleware and the ServeMux observe the
		// same request value, which is what lets metrics read r.Pattern.
		middlewares = append(middlewares, middleware.Timeout(deps.Config.HTTP.RequestTimeout))
	}
	middlewares = append(middlewares,
		middleware.RequestID(),
		middleware.Tracing(),
		middleware.AccessLog(logger),
		middleware.Metrics(deps.Metrics),
		middleware.Recover(logger),
		middleware.SecurityHeaders(),
		middleware.CORS(deps.Config.CORS.AllowedOrigins),
	)

	return middleware.Chain(router, middlewares...)
}
