// Command seed creates the demo login accounts in PostgreSQL.
//
// It applies pending migrations first, so it can run before the API starts or
// as a one-shot compose service. Accounts are upserted by email: running it
// twice rotates the password instead of duplicating rows.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	authapp "github.com/amirfaisalz/procure-flow/internal/application/auth"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/domain/user"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/platform/id"
	"github.com/amirfaisalz/procure-flow/internal/platform/logging"
	"github.com/amirfaisalz/procure-flow/internal/platform/password"
	"github.com/amirfaisalz/procure-flow/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seeding failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	dbConfig, err := config.LoadDatabase()
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}
	if dbConfig.URL == "" {
		return errors.New("DATABASE_URL is required: seed writes accounts to PostgreSQL")
	}

	plain, err := seedPassword()
	if err != nil {
		return err
	}
	if err := password.Validate(plain); err != nil {
		return err
	}

	logger := logging.New(slog.LevelInfo, "seed", os.Stdout)
	slog.SetDefault(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	db, err := postgres.Open(ctx, postgres.Options{
		URL:             dbConfig.URL,
		MaxOpenConns:    dbConfig.MaxOpenConns,
		MaxIdleConns:    dbConfig.MaxIdleConns,
		ConnMaxLifetime: dbConfig.ConnMaxLifetime,
		ConnMaxIdleTime: dbConfig.ConnMaxIdleTime,
	})
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := migrations.Up(ctx, db); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	repository := postgres.NewUserRepository(db)
	hasher := password.NewDefault()
	ids := id.NewGenerator()
	accounts := authapp.DemoAccounts()

	for _, account := range accounts {
		hash, err := hasher.Hash(plain)
		if err != nil {
			return fmt.Errorf("hash password for %s: %w", account.Email, err)
		}

		now := time.Now().UTC()
		record := user.User{
			ID:           ids.NewUserID(),
			Email:        account.Email,
			Name:         account.Name,
			PasswordHash: hash,
			Roles:        account.Roles,
			Department:   account.Department,
			Active:       true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := record.Validate(); err != nil {
			return fmt.Errorf("account %s: %w", account.Email, err)
		}
		if err := repository.Upsert(ctx, record); err != nil {
			return fmt.Errorf("seed %s: %w", account.Email, err)
		}

		logger.Info("account seeded",
			slog.String("email", account.Email),
			slog.Any("roles", account.Roles),
			slog.String("department", account.Department),
		)
	}

	logger.Info("seeding complete",
		slog.Int("accounts", len(accounts)),
		slog.Bool("default_password", strings.TrimSpace(os.Getenv("SEED_PASSWORD")) == ""),
	)
	return nil
}

// seedPassword returns SEED_PASSWORD, falling back to the documented demo
// password only outside production.
func seedPassword() (string, error) {
	if value := strings.TrimSpace(os.Getenv("SEED_PASSWORD")); value != "" {
		return value, nil
	}

	production := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), string(config.EnvProduction))
	if production {
		return "", errors.New("SEED_PASSWORD is required when APP_ENV=production")
	}
	return authapp.DemoPassword, nil
}
