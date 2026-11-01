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

		result, err := repo.List(ctx, audit.ListFilter{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_1",
		})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(result.Events) != 3 {
			t.Fatalf("len(events) = %d, want 3", len(result.Events))
		}
		if result.Total != 3 {
			t.Errorf("Total = %d, want 3", result.Total)
		}

		wantActions := []string{"procurement.approved", "procurement.submitted", "procurement.created"}
		for i, want := range wantActions {
			if result.Events[i].Action != want {
				t.Errorf("events[%d].Action = %q, want %q", i, result.Events[i].Action, want)
			}
			if result.Events[i].ID == 0 {
				t.Errorf("events[%d].ID = 0, want an assigned identifier", i)
			}
		}
		if result.Events[2].Metadata["number"] != "PR-202610-0001" {
			t.Errorf("metadata = %v, want the stored number", result.Events[2].Metadata)
		}
		if !result.Events[2].OccurredAt.Equal(auditBaseTime) {
			t.Errorf("OccurredAt = %v, want %v", result.Events[2].OccurredAt, auditBaseTime)
		}

		otherResult, err := repo.List(ctx, audit.ListFilter{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_2",
		})
		if err != nil {
			t.Fatalf("List(other) error = %v", err)
		}
		if len(otherResult.Events) != 1 {
			t.Errorf("other entity events = %d, want 1", len(otherResult.Events))
		}
	})

	t.Run("Pagination", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		const total = 7
		for i := 0; i < total; i++ {
			if err := repo.Append(ctx, audit.Event{
				EntityType: audit.EntityProcurement,
				EntityID:   "prc_page",
				Action:     fmt.Sprintf("procurement.action_%d", i),
				ActorID:    "user_amir",
				OccurredAt: auditBaseTime.Add(time.Duration(i) * time.Minute),
			}); err != nil {
				t.Fatalf("Append() error = %v", err)
			}
		}

		first, err := repo.List(ctx, audit.ListFilter{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_page",
			Page:       1,
			PageSize:   3,
		})
		if err != nil {
			t.Fatalf("List(page 1) error = %v", err)
		}
		if first.Total != total || first.Page != 1 || first.PageSize != 3 {
			t.Errorf("page 1 meta = %+v, want total=7 page=1 size=3", first)
		}
		if first.TotalPages() != 3 {
			t.Errorf("TotalPages() = %d, want 3", first.TotalPages())
		}
		if len(first.Events) != 3 || first.Events[0].Action != "procurement.action_6" {
			t.Errorf("page 1 events = %v, want the three newest", actions(first.Events))
		}

		last, err := repo.List(ctx, audit.ListFilter{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_page",
			Page:       3,
			PageSize:   3,
		})
		if err != nil {
			t.Fatalf("List(page 3) error = %v", err)
		}
		if len(last.Events) != 1 || last.Events[0].Action != "procurement.action_0" {
			t.Errorf("page 3 events = %v, want the oldest event", actions(last.Events))
		}

		beyond, err := repo.List(ctx, audit.ListFilter{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_page",
			Page:       9,
			PageSize:   3,
		})
		if err != nil {
			t.Fatalf("List(beyond) error = %v", err)
		}
		if len(beyond.Events) != 0 || beyond.Total != total {
			t.Errorf("beyond = %d events (total %d), want an empty page", len(beyond.Events), beyond.Total)
		}
	})

	t.Run("ListEmpty", func(t *testing.T) {
		repo := newRepo(t)
		result, err := repo.List(context.Background(), audit.ListFilter{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_missing",
		})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(result.Events) != 0 {
			t.Errorf("len(events) = %d, want 0", len(result.Events))
		}
		if result.Events == nil {
			t.Error("events = nil, want an empty slice for JSON encoding")
		}
		if result.Total != 0 {
			t.Errorf("Total = %d, want 0", result.Total)
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

		result, err := repo.List(ctx, audit.ListFilter{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_concurrent",
			PageSize:   100,
		})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(result.Events) != workers {
			t.Fatalf("len(events) = %d, want %d", len(result.Events), workers)
		}
		seen := make(map[int64]struct{}, workers)
		for _, event := range result.Events {
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
