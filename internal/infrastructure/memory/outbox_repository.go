package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

// OutboxRepository is a thread-safe, in-memory outbox used for local
// development and tests. It mirrors the leasing semantics of the PostgreSQL
// adapter so the same contract tests apply to both.
type OutboxRepository struct {
	mu      sync.Mutex
	records map[string]*outboxRecord
}

type outboxRecord struct {
	event       event.Event
	nextAttempt time.Time
	attempts    int
	lastError   string
	published   bool
	publishedAt time.Time
}

// NewOutboxRepository creates an empty outbox.
func NewOutboxRepository() *OutboxRepository {
	return &OutboxRepository{records: make(map[string]*outboxRecord)}
}

// Enqueue stores a pending event.
func (r *OutboxRepository) Enqueue(ctx context.Context, ev event.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ev.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.records[ev.ID]; exists {
		return fmt.Errorf("memory: outbox event %s already exists", ev.ID)
	}

	ev.Payload = append([]byte(nil), ev.Payload...)
	r.records[ev.ID] = &outboxRecord{
		event:       ev,
		nextAttempt: ev.OccurredAt,
	}
	return nil
}

// Claim leases up to limit due events, ordered by next attempt time.
func (r *OutboxRepository) Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]event.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	due := make([]*outboxRecord, 0, limit)
	for _, record := range r.records {
		if record.published || record.nextAttempt.After(now) {
			continue
		}
		due = append(due, record)
	}
	sort.Slice(due, func(i, j int) bool {
		if !due[i].nextAttempt.Equal(due[j].nextAttempt) {
			return due[i].nextAttempt.Before(due[j].nextAttempt)
		}
		if !due[i].event.OccurredAt.Equal(due[j].event.OccurredAt) {
			return due[i].event.OccurredAt.Before(due[j].event.OccurredAt)
		}
		return due[i].event.ID < due[j].event.ID
	})

	if len(due) > limit {
		due = due[:limit]
	}

	out := make([]event.Event, 0, len(due))
	for _, record := range due {
		record.nextAttempt = now.Add(lease)

		claimed := record.event
		claimed.Payload = append([]byte(nil), record.event.Payload...)
		claimed.Attempts = record.attempts
		out = append(out, claimed)
	}
	return out, nil
}

// MarkPublished records a successful delivery.
func (r *OutboxRepository) MarkPublished(ctx context.Context, id string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.records[id]
	if !ok || record.published {
		return nil
	}
	record.published = true
	record.publishedAt = at
	record.lastError = ""
	return nil
}

// MarkFailed records an attempt failure and schedules the next attempt.
func (r *OutboxRepository) MarkFailed(ctx context.Context, id, cause string, next time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.records[id]
	if !ok || record.published {
		return nil
	}
	record.attempts++
	record.lastError = truncateCause(cause)
	record.nextAttempt = next
	return nil
}

// Prune deletes published events older than before.
func (r *OutboxRepository) Prune(ctx context.Context, before time.Time) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var removed int64
	for id, record := range r.records {
		if record.published && record.publishedAt.Before(before) {
			delete(r.records, id)
			removed++
		}
	}
	return removed, nil
}

// truncateCause bounds the stored error text.
func truncateCause(cause string) string {
	const maxLength = 500
	cause = strings.TrimSpace(cause)
	if len(cause) <= maxLength {
		return cause
	}
	return cause[:maxLength]
}
