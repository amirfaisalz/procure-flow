package middleware

import (
	"net/http"

	"github.com/amirfaisalz/procure-flow/internal/platform/tracing"
)

// Tracing propagates W3C Trace Context. It adopts an inbound traceparent when
// valid, starts a new span for this request, and exposes the identifiers to
// logs and downstream handlers.
func Tracing() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ids, ok := tracing.ParseTraceparent(r.Header.Get("traceparent"))
			if ok {
				ids = ids.NewSpan()
			} else {
				ids = tracing.New()
			}

			w.Header().Set("traceparent", ids.Traceparent())
			w.Header().Set("X-Trace-ID", ids.TraceID)

			next.ServeHTTP(w, r.WithContext(tracing.WithContext(r.Context(), ids)))
		})
	}
}
