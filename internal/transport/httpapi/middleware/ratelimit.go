package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strconv"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/platform/ratelimit"
	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

// RateLimit rejects requests that exceed the per-caller token bucket. Callers
// are identified by authenticated subject when available, otherwise by remote
// IP address.
func RateLimit(limiter ratelimit.Limiter, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enforceRateLimit(w, r, limiter, rateLimitKey(r), logger) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// enforceRateLimit consumes one token for key and writes a 429 problem when the
// bucket is exhausted. It reports whether the request may continue. Limiter
// failures fail open: the limiter is an availability aid, not an authorization
// boundary.
func enforceRateLimit(
	w http.ResponseWriter,
	r *http.Request,
	limiter ratelimit.Limiter,
	key string,
	logger *slog.Logger,
) bool {
	if limiter == nil {
		return true
	}

	result, err := limiter.Allow(r.Context(), key)
	if err != nil {
		logger.ErrorContext(r.Context(), "rate limiter failed; allowing request",
			slog.String("request_id", requestid.From(r.Context())),
			slog.String("error", err.Error()),
		)
		return true
	}

	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limiter.Burst()))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))

	if result.Allowed {
		return true
	}

	w.Header().Set("Retry-After", RetryAfterSeconds(result.RetryAfter))
	logger.WarnContext(r.Context(), "rate limit exceeded",
		slog.String("request_id", requestid.From(r.Context())),
		slog.String("path", r.URL.Path),
	)
	response.WriteProblem(w, r, response.NewProblem(
		http.StatusTooManyRequests,
		"RATE_LIMITED",
		"Too many requests",
		"the request rate limit has been exceeded",
	))
	return false
}

func rateLimitKey(r *http.Request) string {
	if actor, ok := app.ActorFrom(r.Context()); ok {
		return "actor:" + actor.Subject
	}
	return ipRateLimitKey(r)
}

func ipRateLimitKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return "ip:" + host
	}
	return "ip:" + r.RemoteAddr
}
