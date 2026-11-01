package logging

import (
	"io"
	"log/slog"
	"os"
)

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
