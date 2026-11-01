package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

// Authentication authenticates the request, injects the principal into the
// request context, and rejects unauthenticated callers with a 401 problem.
func Authentication(authenticator auth.Authenticator, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := authenticator.Authenticate(r.Context(), r)
			if err != nil {
				if errors.Is(err, auth.ErrUnauthenticated) {
					w.Header().Set("WWW-Authenticate", `Bearer realm="procure-flow"`)
					response.WriteProblem(w, r, response.NewProblem(
						http.StatusUnauthorized,
						"UNAUTHENTICATED",
						"Authentication required",
						"provide a valid bearer token",
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
				return
			}

			ctx := auth.WithPrincipal(r.Context(), principal)
			ctx = app.WithActor(ctx, ActorFromPrincipal(principal))
			next.ServeHTTP(w, r.WithContext(ctx))
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
		Roles:      roles,
		Department: principal.Department,
	}
}
