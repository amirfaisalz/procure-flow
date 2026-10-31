package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/config"
)

// NewServer builds an http.Server with production-safe timeouts. The listener is
// created separately so startup errors surface before the serve loop begins.
func NewServer(cfg config.HTTP, handler http.Handler, logger *slog.Logger) *http.Server {
	if logger == nil {
		logger = slog.Default()
	}

	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

// Run serves until ctx is canceled, then drains in-flight requests within
// shutdownTimeout. It returns a serve error immediately if the listener fails.
func Run(
	ctx context.Context,
	listener net.Listener,
	srv *http.Server,
	shutdownTimeout time.Duration,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down http server", slog.Duration("timeout", shutdownTimeout))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	return <-serveErr
}
