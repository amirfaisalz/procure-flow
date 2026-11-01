package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/platform/metrics"
)

type routeContextKey struct{}

// routeRecorder captures the most specific ServeMux pattern matched for a
// request. Middleware that replaces the request value (authentication, for
// example) hides the inner mux's r.Pattern from the outer Metrics middleware,
// so nested routers report their pattern through this recorder instead.
type routeRecorder struct {
	pattern string
}

// SetRoute records pattern for the request, keeping the first non-empty value
// so the most specific nested router wins.
func SetRoute(ctx context.Context, pattern string) {
	if pattern == "" {
		return
	}
	recorder, ok := ctx.Value(routeContextKey{}).(*routeRecorder)
	if !ok || recorder.pattern != "" {
		return
	}
	recorder.pattern = pattern
}

// Metrics records request counts and durations. It uses the ServeMux route
// pattern as the label so identifiers never create unbounded cardinality.
func Metrics(registry *metrics.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			recorder := &routeRecorder{}

			next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), routeContextKey{}, recorder)))

			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			route := recorder.pattern
			if route == "" {
				route = r.Pattern
			}
			registry.Observe(r.Method, route, status, time.Since(start))
		})
	}
}
