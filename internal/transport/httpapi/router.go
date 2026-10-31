package httpapi

import (
	"log/slog"
	"net/http"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/middleware"
)

// Dependencies are the collaborators required to build the HTTP router.
type Dependencies struct {
	Config      config.Config
	Logger      *slog.Logger
	Procurement *app.Service
	Health      *handler.Health
}

// NewRouter wires routes and middleware. Middleware is ordered so a request ID
// exists before logging, panics are recovered before headers are finalized, and
// the timeout applies to the innermost handler.
func NewRouter(deps Dependencies) http.Handler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	procurementHandler := handler.NewProcurement(deps.Procurement, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", deps.Health.Live)
	mux.HandleFunc("GET /readyz", deps.Health.Ready)

	mux.HandleFunc("POST /api/v1/procurements", procurementHandler.Create)
	mux.HandleFunc("GET /api/v1/procurements", procurementHandler.List)
	mux.HandleFunc("GET /api/v1/procurements/{id}", procurementHandler.Get)
	mux.HandleFunc("POST /api/v1/procurements/{id}/submit", procurementHandler.Submit)
	mux.HandleFunc("POST /api/v1/procurements/{id}/approve", procurementHandler.Approve)
	mux.HandleFunc("POST /api/v1/procurements/{id}/reject", procurementHandler.Reject)
	mux.HandleFunc("POST /api/v1/procurements/{id}/cancel", procurementHandler.Cancel)

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
