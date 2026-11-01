package repositorytest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
)

var auditBaseTime = time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)

// NewAuditRepository returns an empty audit repository. It must isolate state
// between calls so each subtest starts from a clean slate.
type NewAuditRepository func(t *testing.T) audit.Repository

// RunAudit executes the audit repository contract.
func RunAudit(t *testing.T, newRepo NewAuditRepository) {
	t.Helper()

	t.Run("AppendAndListNewestFirst", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		created := audit.Event{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_1",
			Action:     "procurement.created",
			ActorID:    "user_amir",
			ActorName:  "Amir",
			ActorRole:  "requester",
			ToStatus:   "draft",
			Metadata:   map[string]string{"number": "PR-202610-0001"},
			OccurredAt: auditBaseTime,
		}
		submitted := created
		submitted.Action = "procurement.submitted"
		submitted.FromStatus = "draft"
		submitted.ToStatus = "submitted"
		submitted.OccurredAt = auditBaseTime.Add(time.Minute)
		approved := created
		approved.Action = "procurement.approved"
		approved.ActorID = "user_fin"
		approved.ActorName = "Fin"
		approved.ActorRole = "approver"
		approved.FromStatus = "submitted"
		approved.ToStatus = "approved"
		approved.OccurredAt = auditBaseTime.Add(2 * time.Minute)

		for _, event := range []audit.Event{created, submitted, approved} {
			if err := repo.Append(ctx, event); err != nil {
				t.Fatalf("Append(%s) error = %v", event.Action, err)
			}
		}

		other := created
		other.EntityID = "prc_2"
		other.OccurredAt = auditBaseTime.Add(3 * time.Minute)
		if err := repo.Append(ctx, other); err != nil {
			t.Fatalf("Append(other) error = %v", err)
		}

		events, err := repo.ListByEntity(ctx, audit.EntityProcurement, "prc_1", 0)
		if err != nil {
			t.Fatalf("ListByEntity() error = %v", err)
		}
		if len(events) != 3 {
			t.Fatalf("len(events) = %d, want 3", len(events))
		}

		wantActions := []string{"procurement.approved", "procurement.submitted", "procurement.created"}
		for i, want := range wantActions {
			if events[i].Action != want {
				t.Errorf("events[%d].Action = %q, want %q", i, events[i].Action, want)
			}
			if events[i].ID == 0 {
				t.Errorf("events[%d].ID = 0, want an assigned identifier", i)
			}
		}
		if events[2].Metadata["number"] != "PR-202610-0001" {
			t.Errorf("metadata = %v, want the stored number", events[2].Metadata)
		}
		if !events[2].OccurredAt.Equal(auditBaseTime) {
			t.Errorf("OccurredAt = %v, want %v", events[2].OccurredAt, auditBaseTime)
		}

		limited, err := repo.ListByEntity(ctx, audit.EntityProcurement, "prc_1", 2)
		if err != nil {
			t.Fatalf("ListByEntity(limit) error = %v", err)
		}
		if len(limited) != 2 || limited[0].Action != "procurement.approved" {
			t.Errorf("limited = %v, want the two newest events", actions(limited))
		}

		otherEvents, err := repo.ListByEntity(ctx, audit.EntityProcurement, "prc_2", 0)
		if err != nil {
			t.Fatalf("ListByEntity(other) error = %v", err)
		}
		if len(otherEvents) != 1 {
			t.Errorf("other entity events = %d, want 1", len(otherEvents))
		}
	})

	t.Run("ListEmpty", func(t *testing.T) {
		repo := newRepo(t)
		events, err := repo.ListByEntity(context.Background(), audit.EntityProcurement, "prc_missing", 0)
		if err != nil {
			t.Fatalf("ListByEntity() error = %v", err)
		}
		if len(events) != 0 {
			t.Errorf("len(events) = %d, want 0", len(events))
		}
		if events == nil {
			t.Error("events = nil, want an empty slice for JSON encoding")
		}
	})

	t.Run("ConcurrentAppends", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		const workers = 20
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs <- repo.Append(ctx, audit.Event{
					EntityType: audit.EntityProcurement,
					EntityID:   "prc_concurrent",
					Action:     fmt.Sprintf("procurement.action_%d", i),
					ActorID:    "user_amir",
					OccurredAt: auditBaseTime.Add(time.Duration(i) * time.Second),
				})
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}
		}

		events, err := repo.ListByEntity(ctx, audit.EntityProcurement, "prc_concurrent", 100)
		if err != nil {
			t.Fatalf("ListByEntity() error = %v", err)
		}
		if len(events) != workers {
			t.Fatalf("len(events) = %d, want %d", len(events), workers)
		}
		seen := make(map[int64]struct{}, workers)
		for _, event := range events {
			if _, exists := seen[event.ID]; exists {
				t.Fatalf("duplicate audit id %d", event.ID)
			}
			seen[event.ID] = struct{}{}
		}
	})
}

func actions(events []audit.Event) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = event.Action
	}
	return out
}
