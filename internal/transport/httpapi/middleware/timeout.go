package middleware

import (
	"context"
	"net/http"
	"time"
)

// Timeout attaches a per-request deadline. Handlers and repositories must honor
// the request context for the deadline to take effect.
func Timeout(duration time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
