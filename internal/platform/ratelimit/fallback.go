package ratelimit

import (
	"context"
	"log/slog"
)

// Fallback tries the primary limiter and degrades to the secondary limiter
// when the primary fails. It keeps requests flowing during a Redis outage
// while still enforcing a local limit, instead of failing open entirely.
type Fallback struct {
	primary   Limiter
	secondary Limiter
	logger    *slog.Logger
}

// NewFallback wraps primary with a best-effort secondary limiter.
func NewFallback(primary, secondary Limiter, logger *slog.Logger) *Fallback {
	if logger == nil {
		logger = slog.Default()
	}
	return &Fallback{primary: primary, secondary: secondary, logger: logger}
}

// Allow delegates to the primary limiter and falls back on error.
func (f *Fallback) Allow(ctx context.Context, key string) (Result, error) {
	if f == nil || f.primary == nil {
		return allowWith(f.secondary, ctx, key)
	}

	result, err := f.primary.Allow(ctx, key)
	if err == nil {
		return result, nil
	}

	f.logger.WarnContext(ctx, "rate limiter unavailable; falling back to in-process limiting",
		slog.String("error", err.Error()),
	)
	fallbackResult, fallbackErr := allowWith(f.secondary, ctx, key)
	if fallbackErr == nil {
		return fallbackResult, nil
	}
	// Both limiter backends failed. Prefer availability: let the request
	// through rather than taking the API down with the rate limiter.
	return Result{Allowed: true}, nil
}

// Burst reports the primary limiter's burst, falling back to the secondary.
func (f *Fallback) Burst() int {
	if f == nil {
		return 0
	}
	if f.primary != nil {
		return f.primary.Burst()
	}
	if f.secondary != nil {
		return f.secondary.Burst()
	}
	return 0
}

func allowWith(limiter Limiter, ctx context.Context, key string) (Result, error) {
	if limiter == nil {
		return Result{Allowed: true}, nil
	}
	return limiter.Allow(ctx, key)
}
