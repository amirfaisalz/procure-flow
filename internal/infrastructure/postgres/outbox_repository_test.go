package postgres_test

import (
	"context"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/outbox"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
)

func TestOutboxRepositoryContract(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	repositorytest.RunOutbox(t, func(t *testing.T) outbox.Repository {
		t.Helper()
		if _, err := db.ExecContext(ctx, "TRUNCATE outbox_events"); err != nil {
			t.Fatalf("truncate outbox_events: %v", err)
		}
		return postgres.NewOutboxRepository(db)
	})
}
