package postgres_test

import (
	"context"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
)

func TestIdempotencyRepositoryContract(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	repositorytest.RunIdempotency(t, func(t *testing.T) idempotency.Repository {
		t.Helper()
		if _, err := db.ExecContext(ctx, "TRUNCATE idempotency_keys"); err != nil {
			t.Fatalf("truncate idempotency_keys: %v", err)
		}
		return postgres.NewIdempotencyRepository(db)
	})
}
