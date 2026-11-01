package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
	"github.com/amirfaisalz/procure-flow/internal/platform/ratelimit"
	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

// Authentication authenticates the request and injects the actor into the
// request context. Unauthenticated callers receive a 401 problem.
func Authentication(authenticator auth.Authenticator, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := authenticator.Authenticate(r.Context(), r)
			if err != nil {
				writeAuthError(w, r, logger, err)
				return
			}

			next.ServeHTTP(w, r.WithContext(app.WithActor(r.Context(), ActorFromPrincipal(principal))))
		})
	}
}

// AuthenticatedRateLimit authenticates first and then rate limits in one step,
// so unauthenticated traffic is throttled too. Valid credentials consume the
// actor's bucket; failed authentication consumes a per-IP bucket before the 401
// is written, which prevents credential guessing from bypassing the limiter.
func AuthenticatedRateLimit(authenticator auth.Authenticator, limiter ratelimit.Limiter, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := authenticator.Authenticate(r.Context(), r)
			if err != nil {
				if errors.Is(err, auth.ErrUnauthenticated) &&
					!enforceRateLimit(w, r, limiter, ipRateLimitKey(r), logger) {
					return
				}
				writeAuthError(w, r, logger, err)
				return
			}

			if !enforceRateLimit(w, r, limiter, "actor:"+principal.Subject, logger) {
				return
			}
			next.ServeHTTP(w, r.WithContext(app.WithActor(r.Context(), ActorFromPrincipal(principal))))
		})
	}
}

// InjectActor attaches a fixed actor without authentication. It is used only
// when authentication is disabled in local development.
func InjectActor(actor app.Actor) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(app.WithActor(r.Context(), actor)))
		})
	}
}

// ActorFromPrincipal maps an authenticated principal onto the application's
// actor model.
func ActorFromPrincipal(principal auth.Principal) app.Actor {
	roles := make([]string, len(principal.Roles))
	for i, role := range principal.Roles {
		roles[i] = string(role)
	}
	return app.Actor{
		Subject:    principal.Subject,
		Name:       principal.Name,
		Email:      principal.Email,
		Roles:      roles,
		Department: principal.Department,
	}
}

func writeAuthError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	if errors.Is(err, auth.ErrUnauthenticated) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="procure-flow"`)
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusUnauthorized,
			"UNAUTHENTICATED",
			"Authentication required",
			"provide a valid session cookie or bearer token",
		))
		return
	}

	logger.ErrorContext(r.Context(), "authentication error",
		slog.String("error", err.Error()),
		slog.String("request_id", requestid.From(r.Context())),
	)
	response.WriteProblem(w, r, response.NewProblem(
		http.StatusInternalServerError,
		"INTERNAL_ERROR",
		"Internal server error",
		"an unexpected error occurred",
	))
}
