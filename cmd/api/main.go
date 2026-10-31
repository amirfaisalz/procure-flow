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
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/platform/id"
	"github.com/amirfaisalz/procure-flow/internal/platform/logging"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
	"github.com/amirfaisalz/procure-flow/migrations"
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

	repo, ping, closeRepository, err := openRepository(cfg, logger)
	if err != nil {
		return err
	}
	if closeRepository != nil {
		defer func() { _ = closeRepository() }()
	}

	service := app.NewService(repo, id.NewGenerator(), nil)
	health := handler.NewHealth(version, map[string]handler.Check{
		"repository": ping,
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

// openRepository selects PostgreSQL when DATABASE_URL is set and falls back to
// the in-memory adapter otherwise. Production requires a database (enforced by
// config validation).
func openRepository(cfg config.Config, logger *slog.Logger) (domainproc.Repository, func(context.Context) error, func() error, error) {
	if cfg.Database.URL == "" {
		repo := memory.NewProcurementRepository()
		logger.Warn("using in-memory repository; data will not survive a restart")
		return repo, repo.Ping, nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, postgres.Options{
		URL:             cfg.Database.URL,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
		ConnMaxIdleTime: cfg.Database.ConnMaxIdleTime,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if cfg.Database.AutoMigrate {
		if err := migrations.Up(ctx, db); err != nil {
			_ = db.Close()
			return nil, nil, nil, fmt.Errorf("apply migrations: %w", err)
		}
		logger.Info("database migrations applied")
	}

	logger.Info("using postgres repository")
	return postgres.NewProcurementRepository(db), db.PingContext, db.Close, nil
}
