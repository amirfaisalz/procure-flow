package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
)

func TestAuditRepositoryContract(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	repositorytest.RunAudit(t, func(t *testing.T) audit.Repository {
		t.Helper()
		if _, err := db.ExecContext(ctx, "TRUNCATE audit_events"); err != nil {
			t.Fatalf("truncate audit_events: %v", err)
		}
		return postgres.NewAuditRepository(db)
	})
}

func TestAuditEventsAreAppendOnly(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, "TRUNCATE audit_events"); err != nil {
		t.Fatalf("truncate audit_events: %v", err)
	}

	repo := postgres.NewAuditRepository(db)
	if err := repo.Append(ctx, audit.Event{
		EntityType: audit.EntityProcurement,
		EntityID:   "prc_1",
		Action:     "procurement.created",
		ActorID:    "user_amir",
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if _, err := db.ExecContext(ctx, "UPDATE audit_events SET action = 'tampered'"); err == nil {
		t.Fatal("UPDATE audit_events succeeded, want the append-only trigger to reject it")
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM audit_events"); err == nil {
		t.Fatal("DELETE FROM audit_events succeeded, want the append-only trigger to reject it")
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&count); err != nil {
		t.Fatalf("count audit_events: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}
