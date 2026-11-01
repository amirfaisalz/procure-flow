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

	goredis "github.com/redis/go-redis/v9"

	authapp "github.com/amirfaisalz/procure-flow/internal/application/auth"
	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
	"github.com/amirfaisalz/procure-flow/internal/domain/outbox"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/domain/user"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/rabbitmq"
	redisstore "github.com/amirfaisalz/procure-flow/internal/infrastructure/redis"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
	"github.com/amirfaisalz/procure-flow/internal/platform/id"
	"github.com/amirfaisalz/procure-flow/internal/platform/logging"
	"github.com/amirfaisalz/procure-flow/internal/platform/metrics"
	"github.com/amirfaisalz/procure-flow/internal/platform/password"
	"github.com/amirfaisalz/procure-flow/internal/platform/ratelimit"
	"github.com/amirfaisalz/procure-flow/internal/platform/relay"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Only the repository is required for readiness. Redis and RabbitMQ are
	// optimizations: report them, but do not let their outage pull every
	// replica out of the load balancer.
	checks := map[string]handler.Check{
		"repository": store.ping,
	}
	optionalChecks := map[string]handler.Check{}
	serviceOptions := []app.Option{
		app.WithAudit(store.audit, store.uow),
	}

	redisClient, err := openRedis(cfg, logger, optionalChecks)
	if err != nil {
		return err
	}
	if redisClient != nil {
		defer func() { _ = redisClient.Close() }()
		if cfg.Redis.CacheEnabled {
			serviceOptions = append(serviceOptions,
				app.WithCache(redisstore.NewCache(redisClient, "procure:"), cfg.Redis.CacheTTL),
			)
			logger.Info("aggregate cache enabled", slog.Duration("ttl", cfg.Redis.CacheTTL))
		}
	}

	// The outbox exists to feed the broker. Without a broker URL the service
	// skips event emission entirely so rows cannot accumulate forever.
	var publisher *rabbitmq.Publisher
	var relayStopped <-chan struct{}
	if cfg.RabbitMQ.URL != "" {
		serviceOptions = append(serviceOptions, app.WithOutbox(store.outbox))

		publisher = rabbitmq.NewPublisher(cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange)
		defer func() { _ = publisher.Close() }()
		optionalChecks["rabbitmq"] = publisher.Healthy

		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := publisher.Connect(connectCtx); err != nil {
			logger.Warn("rabbitmq is not reachable; events stay queued in the outbox until it recovers",
				slog.String("error", err.Error()))
		}
		cancel()

		relayer := relay.New(store.outbox, publisher, logger, relay.DefaultConfig())
		stopped := make(chan struct{})
		relayStopped = stopped
		go func() {
			defer close(stopped)
			relayer.Run(ctx)
		}()
		logger.Info("outbox relay started", slog.String("exchange", cfg.RabbitMQ.Exchange))
	}

	service := app.NewService(store.procurements, id.NewGenerator(), nil, serviceOptions...)
	health := handler.NewHealthWithOptional(version, checks, optionalChecks)

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

	// Session login needs a signing key. Without one the API keeps accepting
	// bearer credentials only; /me stays available, login and logout do not.
	var authHandler *handler.Auth
	if cfg.Auth.Enabled {
		var loginService *authapp.Service
		if cfg.Auth.JWTSecret != "" {
			hasher := password.NewDefault()
			issuer := auth.NewJWTIssuer([]byte(cfg.Auth.JWTSecret), cfg.Auth.JWTIssuer, cfg.Auth.JWTAudience)
			loginService = authapp.NewService(store.users, hasher, issuer, cfg.Auth.SessionTTL)
		}

		authHandler = handler.NewAuth(loginService, handler.AuthConfig{
			CookieName:   cfg.Auth.CookieName,
			CookieSecure: cfg.Auth.CookieSecure,
		}, logger)

		if loginService != nil {
			logger.Info("session login enabled",
				slog.String("cookie", cfg.Auth.CookieName),
				slog.Bool("secure_cookie", cfg.Auth.CookieSecure),
				slog.Duration("session_ttl", cfg.Auth.SessionTTL),
			)
			if !cfg.Auth.CookieSecure && cfg.Env != config.EnvDevelopment {
				logger.Warn("session cookie is not marked Secure; terminate TLS and set AUTH_COOKIE_SECURE=true")
			}
		} else {
			logger.Info("session login disabled; set AUTH_JWT_SECRET to enable it")
		}
	}

	registry := metrics.NewRegistry()
	limiter := newLimiter(cfg, redisClient, logger)

	router := httpapi.NewRouter(httpapi.Dependencies{
		Config:        cfg,
		Logger:        logger,
		Procurement:   service,
		Health:        health,
		Authenticator: authenticator,
		Auth:          authHandler,
		Idempotency:   store.idempotency,
		RateLimiter:   limiter,
		Metrics:       registry,
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

	runErr := httpapi.Run(ctx, listener, server, cfg.HTTP.ShutdownTimeout, logger)
	// Cancel the context even when Serve failed before a signal arrived, so the
	// relay does not outlive the server. stop also unregisters the signal
	// handlers; the deferred call is then a no-op.
	stop()
	if relayStopped != nil {
		select {
		case <-relayStopped:
		case <-time.After(cfg.HTTP.ShutdownTimeout):
			logger.Warn("outbox relay did not stop within the shutdown budget")
		}
	}
	return runErr
}

// openRedis creates the shared Redis client and registers its optional health
// check. A failed first ping is not fatal: the client reconnects on demand and
// the rate limiter falls back to its in-process bucket meanwhile.
func openRedis(cfg config.Config, logger *slog.Logger, optional map[string]handler.Check) (*goredis.Client, error) {
	if cfg.Redis.URL == "" {
		return nil, nil
	}

	client, err := redisstore.NewClient(cfg.Redis.URL, cfg.Redis.DialTimeout, cfg.Redis.CommandTimeout)
	if err != nil {
		return nil, fmt.Errorf("connect to redis: %w", err)
	}
	optional["redis"] = func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), cfg.Redis.DialTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		logger.Warn("redis is not reachable; the limiter will fall back to in-process buckets until it recovers",
			slog.String("error", err.Error()),
		)
	}
	return client, nil
}

// newLimiter selects the configured limiter. Redis mode keeps a local bucket
// as a fallback so a Redis outage degrades to per-replica limiting instead of
// removing the limit entirely.
func newLimiter(cfg config.Config, client *goredis.Client, logger *slog.Logger) ratelimit.Limiter {
	if !cfg.RateLimit.Enabled {
		return nil
	}

	local := ratelimit.New(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	useRedis := cfg.RateLimit.Store == config.RateLimitStoreRedis ||
		(cfg.RateLimit.Store == config.RateLimitStoreAuto && client != nil)
	if !useRedis || client == nil {
		logger.Info("rate limiting enabled (in-process)",
			slog.Float64("requests_per_second", cfg.RateLimit.RequestsPerSecond),
			slog.Int("burst", cfg.RateLimit.Burst),
		)
		return local
	}

	logger.Info("rate limiting enabled (redis, with in-process fallback)",
		slog.Float64("requests_per_second", cfg.RateLimit.RequestsPerSecond),
		slog.Int("burst", cfg.RateLimit.Burst),
	)
	distributed := ratelimit.NewRedis(client, cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	return ratelimit.NewFallback(distributed, local, logger)
}

// storage bundles the adapters selected at startup.
type storage struct {
	procurements domainproc.Repository
	users        user.Repository
	audit        audit.Repository
	idempotency  idempotency.Repository
	outbox       outbox.Repository
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
			users:        memory.NewUserRepository(),
			audit:        memory.NewAuditRepository(),
			idempotency:  memory.NewIdempotencyRepository(),
			outbox:       memory.NewOutboxRepository(),
			ping:         repo.Ping,
		}, nil
	}

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelConnect()

	db, err := postgres.Open(connectCtx, postgres.Options{
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
		// Migrations can wait on another replica's advisory lock, so they get a
		// separate budget from the connection attempt.
		migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 5*time.Minute)
		err := migrations.Up(migrateCtx, db)
		cancelMigrate()
		if err != nil {
			_ = db.Close()
			return storage{}, fmt.Errorf("apply migrations: %w", err)
		}
		logger.Info("database migrations applied")
	}

	logger.Info("using postgres repository")
	return storage{
		procurements: postgres.NewProcurementRepository(db),
		users:        postgres.NewUserRepository(db),
		audit:        postgres.NewAuditRepository(db),
		idempotency:  postgres.NewIdempotencyRepository(db),
		outbox:       postgres.NewOutboxRepository(db),
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
			Secret:     []byte(cfg.JWTSecret),
			Issuer:     cfg.JWTIssuer,
			Audience:   cfg.JWTAudience,
			Leeway:     cfg.JWTLeeway,
			CookieName: cfg.CookieName,
		}))
	}

	return chain
}
