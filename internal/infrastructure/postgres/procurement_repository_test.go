package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
	"github.com/amirfaisalz/procure-flow/migrations"
)

// testDB connects to TEST_DATABASE_URL and applies migrations. Tests are
// skipped when the variable is unset so `go test ./...` stays hermetic.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("database is not reachable: %v", err)
	}
	if err := migrations.Up(ctx, db); err != nil {
		t.Fatalf("migrations.Up() error = %v", err)
	}
	return db
}

func TestProcurementRepositoryContract(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	repositorytest.Run(t, func(t *testing.T) procurement.Repository {
		t.Helper()
		if _, err := db.ExecContext(ctx, "TRUNCATE procurements"); err != nil {
			t.Fatalf("truncate procurements: %v", err)
		}
		return postgres.NewProcurementRepository(db)
	})
}

func TestMigrationsUpIsIdempotent(t *testing.T) {
	db := testDB(t)

	if err := migrations.Up(context.Background(), db); err != nil {
		t.Fatalf("second migrations.Up() error = %v", err)
	}

	statuses, err := migrations.List(context.Background(), db)
	if err != nil {
		t.Fatalf("migrations.List() error = %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("migrations.List() returned no migrations")
	}
	for _, status := range statuses {
		if !status.Applied {
			t.Errorf("migration %s is not applied", status.Version)
		}
	}
}
