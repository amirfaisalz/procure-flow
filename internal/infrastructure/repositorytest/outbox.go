package repositorytest

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
	"github.com/amirfaisalz/procure-flow/internal/domain/outbox"
)

var outboxBaseTime = time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC)

// NewOutboxRepository returns an empty outbox. It must isolate state between
// calls so each subtest starts from a clean slate.
type NewOutboxRepository func(t *testing.T) outbox.Repository

func outboxEvent(id string, at time.Time) event.Event {
	return event.Event{
		ID:            id,
		Type:          "procurement.created",
		AggregateType: "procurement",
		AggregateID:   "prc_" + id,
		ActorID:       "user_amir",
		OccurredAt:    at,
		Payload:       []byte(`{"id":"prc_` + id + `"}`),
	}
}

func outboxIDs(events []event.Event) []string {
	ids := make([]string, len(events))
	for i, ev := range events {
		ids[i] = ev.ID
	}
	return ids
}

// RunOutbox executes the transactional outbox contract against an adapter.
func RunOutbox(t *testing.T, newRepo NewOutboxRepository) {
	t.Helper()

	t.Run("EnqueueAndClaim", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		events := []event.Event{
			outboxEvent("e1", outboxBaseTime),
			outboxEvent("e2", outboxBaseTime.Add(time.Minute)),
			outboxEvent("e3", outboxBaseTime.Add(2*time.Minute)),
		}
		for _, ev := range events {
			if err := repo.Enqueue(ctx, ev); err != nil {
				t.Fatalf("Enqueue(%s) error = %v", ev.ID, err)
			}
		}

		claimed, err := repo.Claim(ctx, 10, outboxBaseTime.Add(10*time.Minute), time.Minute)
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if len(claimed) != len(events) {
			t.Fatalf("claimed %d events, want %d", len(claimed), len(events))
		}
		for i, ev := range claimed {
			if ev.ID != events[i].ID {
				t.Errorf("claimed[%d].ID = %q, want %q (ordered by occurrence)", i, ev.ID, events[i].ID)
			}
			var gotPayload, wantPayload any
			if err := json.Unmarshal(ev.Payload, &gotPayload); err != nil {
				t.Fatalf("claimed[%d] payload is not JSON: %v", i, err)
			}
			if err := json.Unmarshal(events[i].Payload, &wantPayload); err != nil {
				t.Fatalf("seed payload is not JSON: %v", err)
			}
			if !reflect.DeepEqual(gotPayload, wantPayload) {
				t.Errorf("claimed[%d].Payload = %s, want %s", i, ev.Payload, events[i].Payload)
			}
		}
	})

	t.Run("ClaimRespectsLimit", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		for i, ev := range []event.Event{
			outboxEvent("e1", outboxBaseTime),
			outboxEvent("e2", outboxBaseTime.Add(time.Minute)),
			outboxEvent("e3", outboxBaseTime.Add(2*time.Minute)),
		} {
			if err := repo.Enqueue(ctx, ev); err != nil {
				t.Fatalf("Enqueue(%d) error = %v", i, err)
			}
		}

		claimed, err := repo.Claim(ctx, 2, outboxBaseTime.Add(10*time.Minute), time.Minute)
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if len(claimed) != 2 {
			t.Fatalf("claimed %d events, want 2", len(claimed))
		}
	})

	t.Run("ClaimLeasesEvents", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		if err := repo.Enqueue(ctx, outboxEvent("e1", outboxBaseTime)); err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}

		first, err := repo.Claim(ctx, 10, outboxBaseTime, time.Minute)
		if err != nil {
			t.Fatalf("first Claim() error = %v", err)
		}
		if len(first) != 1 {
			t.Fatalf("first claim returned %d events, want 1", len(first))
		}

		second, err := repo.Claim(ctx, 10, outboxBaseTime.Add(30*time.Second), time.Minute)
		if err != nil {
			t.Fatalf("second Claim() error = %v", err)
		}
		if len(second) != 0 {
			t.Fatalf("second claim returned %d events while the lease is active", len(second))
		}

		third, err := repo.Claim(ctx, 10, outboxBaseTime.Add(2*time.Minute), time.Minute)
		if err != nil {
			t.Fatalf("third Claim() error = %v", err)
		}
		if len(third) != 1 {
			t.Fatalf("event was not claimable after the lease expired")
		}
	})

	t.Run("MarkPublishedRemovesFromPending", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		if err := repo.Enqueue(ctx, outboxEvent("e1", outboxBaseTime)); err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}
		if _, err := repo.Claim(ctx, 10, outboxBaseTime, time.Minute); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if err := repo.MarkPublished(ctx, "e1", outboxBaseTime.Add(time.Second)); err != nil {
			t.Fatalf("MarkPublished() error = %v", err)
		}

		claimed, err := repo.Claim(ctx, 10, outboxBaseTime.Add(time.Hour), time.Minute)
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if len(claimed) != 0 {
			t.Fatalf("published event was claimed again: %+v", claimed)
		}
	})

	t.Run("MarkFailedBacksOff", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		if err := repo.Enqueue(ctx, outboxEvent("e1", outboxBaseTime)); err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}

		if _, err := repo.Claim(ctx, 10, outboxBaseTime, time.Minute); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if err := repo.MarkFailed(ctx, "e1", "broker unavailable", outboxBaseTime.Add(time.Minute)); err != nil {
			t.Fatalf("MarkFailed() error = %v", err)
		}

		early, err := repo.Claim(ctx, 10, outboxBaseTime.Add(30*time.Second), time.Minute)
		if err != nil {
			t.Fatalf("early Claim() error = %v", err)
		}
		if len(early) != 0 {
			t.Fatalf("failed event was claimable before its backoff elapsed")
		}

		retried, err := repo.Claim(ctx, 10, outboxBaseTime.Add(2*time.Minute), time.Minute)
		if err != nil {
			t.Fatalf("retry Claim() error = %v", err)
		}
		if len(retried) != 1 {
			t.Fatalf("retry claim returned %d events, want 1", len(retried))
		}
		if retried[0].Attempts != 1 {
			t.Errorf("Attempts = %d, want 1", retried[0].Attempts)
		}
	})

	t.Run("ClaimOrdersByOccurrenceAfterRetry", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		for _, ev := range []event.Event{
			outboxEvent("e1", outboxBaseTime),
			outboxEvent("e2", outboxBaseTime.Add(time.Minute)),
		} {
			if err := repo.Enqueue(ctx, ev); err != nil {
				t.Fatalf("Enqueue(%s) error = %v", ev.ID, err)
			}
		}

		if _, err := repo.Claim(ctx, 10, outboxBaseTime, time.Minute); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if err := repo.MarkFailed(ctx, "e1", "broker unavailable", outboxBaseTime.Add(2*time.Minute)); err != nil {
			t.Fatalf("MarkFailed() error = %v", err)
		}

		claimed, err := repo.Claim(ctx, 10, outboxBaseTime.Add(3*time.Minute), time.Minute)
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if len(claimed) != 2 || claimed[0].ID != "e1" || claimed[1].ID != "e2" {
			t.Errorf("claimed = %v, want [e1 e2] (occurrence order)", outboxIDs(claimed))
		}
	})

	t.Run("PruneOnlyPublished", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		for _, ev := range []event.Event{
			outboxEvent("published", outboxBaseTime),
			outboxEvent("pending", outboxBaseTime.Add(time.Minute)),
		} {
			if err := repo.Enqueue(ctx, ev); err != nil {
				t.Fatalf("Enqueue(%s) error = %v", ev.ID, err)
			}
		}
		if err := repo.MarkPublished(ctx, "published", outboxBaseTime.Add(time.Second)); err != nil {
			t.Fatalf("MarkPublished() error = %v", err)
		}

		removed, err := repo.Prune(ctx, outboxBaseTime.Add(time.Hour))
		if err != nil {
			t.Fatalf("Prune() error = %v", err)
		}
		if removed != 1 {
			t.Errorf("Prune() removed %d events, want 1", removed)
		}

		claimed, err := repo.Claim(ctx, 10, outboxBaseTime.Add(10*time.Minute), time.Minute)
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if len(claimed) != 1 || claimed[0].ID != "pending" {
			t.Fatalf("claimed = %+v, want the pending event only", claimed)
		}
	})

	t.Run("EnqueueRejectsInvalidEvent", func(t *testing.T) {
		repo := newRepo(t)
		invalid := outboxEvent("e1", outboxBaseTime)
		invalid.Type = ""

		if err := repo.Enqueue(context.Background(), invalid); err == nil {
			t.Fatal("Enqueue() error = nil, want validation error")
		}
	})
}
