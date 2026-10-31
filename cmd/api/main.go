// Command api starts the procure-flow HTTP server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/platform/id"
	"github.com/amirfaisalz/procure-flow/internal/platform/logging"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("application failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := logging.New(cfg.LogLevel, string(cfg.Env), os.Stdout)
	slog.SetDefault(logger)

	repo := memory.NewProcurementRepository()
	service := app.NewService(repo, id.NewGenerator(), nil)
	health := handler.NewHealth(version, map[string]handler.Check{
		"repository": repo.Ping,
	})

	router := httpapi.NewRouter(httpapi.Dependencies{
		Config:      cfg,
		Logger:      logger,
		Procurement: service,
		Health:      health,
	})
	server := httpapi.NewServer(cfg.HTTP, router, logger)

	listener, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTP.Addr, err)
	}

	logger.Info("http server listening",
		slog.String("addr", listener.Addr().String()),
		slog.String("env", string(cfg.Env)),
		slog.String("version", version),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return httpapi.Run(ctx, listener, server, cfg.HTTP.ShutdownTimeout, logger)
}
