// Command migrate applies, rolls back, or reports database migrations.
//
// Usage:
//
//	go run ./cmd/migrate up
//	go run ./cmd/migrate down
//	go run ./cmd/migrate status
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if cfg.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, postgres.Options{
		URL:             cfg.Database.URL,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
		ConnMaxIdleTime: cfg.Database.ConnMaxIdleTime,
	})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch command {
	case "up":
		if err := migrations.Up(ctx, db); err != nil {
			return err
		}
		fmt.Println("migrations applied")
	case "down":
		rolledBack, err := migrations.Down(ctx, db)
		if err != nil {
			return err
		}
		if rolledBack {
			fmt.Println("last migration rolled back")
		} else {
			fmt.Println("no migrations to roll back")
		}
	case "status":
		statuses, err := migrations.List(ctx, db)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			state := "pending"
			if status.Applied {
				state = "applied " + status.AppliedAt.Format(time.RFC3339)
			}
			fmt.Printf("%s_%s\t%s\n", status.Version, status.Name, state)
		}
	default:
		return fmt.Errorf("unknown command %q (want up, down, or status)", command)
	}

	return nil
}
