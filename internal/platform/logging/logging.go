package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
)

type contextKey struct{}

// New builds the application logger. Production and staging use JSON; local
// environments use human-readable text with source locations.
func New(level slog.Level, env string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: env == "development",
	}

	var handler slog.Handler
	switch env {
	case "production", "staging":
		handler = slog.NewJSONHandler(w, opts)
	default:
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(handler)
}

// WithLogger stores a request-scoped logger in the context.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// From returns the request-scoped logger or the process default.
func From(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}
