package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

// Recover converts panics into structured 500 responses. It re-panics with
// http.ErrAbortHandler so the server can suppress stack traces for deliberate
// aborts.
func Recover(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w}

			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}

				logger.ErrorContext(r.Context(), "panic recovered",
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
					slog.String("request_id", requestid.From(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
				)

				if !sw.wroteHeader {
					response.WriteProblem(sw, r, response.NewProblem(
						http.StatusInternalServerError,
						"INTERNAL_ERROR",
						"Internal server error",
						"an unexpected error occurred",
					))
				}
			}()

			next.ServeHTTP(sw, r)
		})
	}
}
