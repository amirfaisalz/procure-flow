package httpapi

import (
	"log/slog"
	"net/http"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
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
}

// localActor is injected when authentication is disabled. It is an admin so
// local development keeps the full workflow available without credentials.
var localActor = app.Actor{
	Subject: "local-admin",
	Name:    "Local Admin",
	Roles:   []string{app.RoleAdmin},
}

// NewRouter wires routes and middleware. Middleware is ordered so a request ID
// exists before logging, panics are recovered before headers are finalized, and
// the timeout applies to the innermost handler.
//
// Health probes and API documentation stay public; every /api/v1 route is
// authenticated. When authentication is disabled the router injects a local
// admin actor, mirroring the behaviour of the in-memory repository.
func NewRouter(deps Dependencies) http.Handler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	procurementHandler := handler.NewProcurement(deps.Procurement, logger)

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/v1/procurements", procurementHandler.Create)
	apiMux.HandleFunc("GET /api/v1/procurements", procurementHandler.List)
	apiMux.HandleFunc("GET /api/v1/procurements/{id}", procurementHandler.Get)
	apiMux.HandleFunc("GET /api/v1/procurements/{id}/audit", procurementHandler.Audit)
	apiMux.HandleFunc("POST /api/v1/procurements/{id}/submit", procurementHandler.Submit)
	apiMux.HandleFunc("POST /api/v1/procurements/{id}/approve", procurementHandler.Approve)
	apiMux.HandleFunc("POST /api/v1/procurements/{id}/reject", procurementHandler.Reject)
	apiMux.HandleFunc("POST /api/v1/procurements/{id}/cancel", procurementHandler.Cancel)

	var apiHandler http.Handler = &fallback{mux: apiMux}
	if deps.Config.Auth.Enabled {
		authenticator := deps.Authenticator
		if authenticator == nil {
			// Fail closed: a missing authenticator rejects every request.
			authenticator = auth.Chain{}
		}
		apiHandler = middleware.Chain(apiHandler, middleware.Authentication(authenticator, logger))
	} else {
		logger.Warn("authentication is disabled; injecting a local admin actor")
		apiHandler = middleware.Chain(apiHandler, middleware.InjectActor(localActor))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", deps.Health.Live)
	mux.HandleFunc("GET /readyz", deps.Health.Ready)
	mux.Handle("/api/v1/", apiHandler)

	if deps.Config.Docs.Enabled {
		docs := newDocsHandler()
		mux.HandleFunc("GET /docs", docs.Index)
		mux.HandleFunc("GET /openapi.yaml", docs.Spec)
	}

	var router http.Handler = &fallback{mux: mux}

	middlewares := []middleware.Middleware{
		middleware.RequestID(),
		middleware.AccessLog(logger),
		middleware.Recover(logger),
		middleware.SecurityHeaders(),
		middleware.CORS(deps.Config.CORS.AllowedOrigins),
	}
	if deps.Config.HTTP.RequestTimeout > 0 {
		middlewares = append(middlewares, middleware.Timeout(deps.Config.HTTP.RequestTimeout))
	}

	return middleware.Chain(router, middlewares...)
}
