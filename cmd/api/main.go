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
	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
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

	store, err := openStorage(cfg, logger)
	if err != nil {
		return err
	}
	if store.close != nil {
		defer func() { _ = store.close() }()
	}

	service := app.NewService(
		store.procurements,
		id.NewGenerator(),
		nil,
		app.WithAudit(store.audit, store.uow),
	)
	health := handler.NewHealth(version, map[string]handler.Check{
		"repository": store.ping,
	})

	var authenticator auth.Authenticator
	if cfg.Auth.Enabled {
		authenticator = newAuthenticator(cfg.Auth)
		logger.Info("authentication enabled",
			slog.Bool("api_keys", len(cfg.Auth.APIKeys) > 0),
			slog.Bool("jwt", cfg.Auth.JWTSecret != ""),
		)
	} else {
		logger.Warn("authentication disabled; all requests act as a local admin")
	}

	router := httpapi.NewRouter(httpapi.Dependencies{
		Config:        cfg,
		Logger:        logger,
		Procurement:   service,
		Health:        health,
		Authenticator: authenticator,
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

// storage bundles the adapters selected at startup.
type storage struct {
	procurements domainproc.Repository
	audit        audit.Repository
	uow          app.UnitOfWork
	ping         func(context.Context) error
	close        func() error
}

// openStorage selects PostgreSQL when DATABASE_URL is set and falls back to the
// in-memory adapters otherwise. Production requires a database (enforced by
// config validation).
func openStorage(cfg config.Config, logger *slog.Logger) (storage, error) {
	if cfg.Database.URL == "" {
		repo := memory.NewProcurementRepository()
		logger.Warn("using in-memory repository; data will not survive a restart")
		return storage{
			procurements: repo,
			audit:        memory.NewAuditRepository(),
			ping:         repo.Ping,
		}, nil
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
		return storage{}, fmt.Errorf("connect to postgres: %w", err)
	}

	if cfg.Database.AutoMigrate {
		if err := migrations.Up(ctx, db); err != nil {
			_ = db.Close()
			return storage{}, fmt.Errorf("apply migrations: %w", err)
		}
		logger.Info("database migrations applied")
	}

	logger.Info("using postgres repository")
	return storage{
		procurements: postgres.NewProcurementRepository(db),
		audit:        postgres.NewAuditRepository(db),
		uow:          postgres.NewUnitOfWork(db),
		ping:         db.PingContext,
		close:        db.Close,
	}, nil
}

// newAuthenticator builds the credential chain from configuration. Static API
// keys are tried first, then HS256 JWTs.
func newAuthenticator(cfg config.Auth) auth.Authenticator {
	chain := auth.Chain{}

	if len(cfg.APIKeys) > 0 {
		keys := make([]auth.APIKey, len(cfg.APIKeys))
		for i, key := range cfg.APIKeys {
			keys[i] = auth.APIKey{
				Token: key.Token,
				Principal: auth.Principal{
					Subject:    key.Subject,
					Name:       key.Name,
					Roles:      key.Roles,
					Department: key.Department,
				},
			}
		}
		chain = append(chain, auth.NewAPIKeyAuthenticator(keys))
	}

	if cfg.JWTSecret != "" {
		chain = append(chain, auth.NewJWTAuthenticator(auth.JWTConfig{
			Secret:   []byte(cfg.JWTSecret),
			Issuer:   cfg.JWTIssuer,
			Audience: cfg.JWTAudience,
			Leeway:   cfg.JWTLeeway,
		}))
	}

	return chain
}
