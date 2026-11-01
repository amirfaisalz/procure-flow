package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

// OutboxRepository is the PostgreSQL transactional outbox. Events are inserted
// inside the caller's transaction and leased by the relay with SKIP LOCKED so
// several API replicas can relay concurrently without double-leasing a row.
type OutboxRepository struct {
	db *sql.DB
}

// NewOutboxRepository creates an outbox repository backed by db.
func NewOutboxRepository(db *sql.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// Enqueue inserts a pending event.
func (r *OutboxRepository) Enqueue(ctx context.Context, ev event.Event) error {
	if err := ev.Validate(); err != nil {
		return err
	}

	_, err := querierFrom(ctx, r.db).ExecContext(ctx, `
INSERT INTO outbox_events (
	id, event_type, aggregate_type, aggregate_id, actor_id, payload,
	occurred_at, next_attempt_at
) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)`,
		ev.ID, ev.Type, ev.AggregateType, ev.AggregateID, ev.ActorID,
		string(ev.Payload), ev.OccurredAt.UTC(), ev.OccurredAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("postgres: enqueue outbox event: %w", err)
	}
	return nil
}

// Claim leases due, unpublished events to this relay until now+lease.
func (r *OutboxRepository) Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]event.Event, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := querierFrom(ctx, r.db).QueryContext(ctx, `
WITH claimable AS (
	SELECT id FROM outbox_events
	WHERE published_at IS NULL AND next_attempt_at <= $1
	ORDER BY next_attempt_at, occurred_at, id
	LIMIT $2
	FOR UPDATE SKIP LOCKED
)
UPDATE outbox_events AS o
SET next_attempt_at = $3
FROM claimable
WHERE o.id = claimable.id
RETURNING o.id, o.event_type, o.aggregate_type, o.aggregate_id, o.actor_id,
          o.payload, o.occurred_at, o.attempts`,
		now.UTC(), limit, now.Add(lease).UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: claim outbox events: %w", err)
	}
	defer rows.Close()

	events := make([]event.Event, 0, limit)
	for rows.Next() {
		var (
			ev      event.Event
			payload []byte
		)
		if err := rows.Scan(
			&ev.ID, &ev.Type, &ev.AggregateType, &ev.AggregateID, &ev.ActorID,
			&payload, &ev.OccurredAt, &ev.Attempts,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan outbox event: %w", err)
		}
		ev.Payload = payload
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: claim outbox events: %w", err)
	}

	sort.Slice(events, func(i, j int) bool {
		if !events[i].OccurredAt.Equal(events[j].OccurredAt) {
			return events[i].OccurredAt.Before(events[j].OccurredAt)
		}
		return events[i].ID < events[j].ID
	})
	return events, nil
}

// MarkPublished records a successful delivery. Already-published events are
// ignored so a relay retry after a crash stays idempotent.
func (r *OutboxRepository) MarkPublished(ctx context.Context, id string, at time.Time) error {
	_, err := querierFrom(ctx, r.db).ExecContext(ctx, `
UPDATE outbox_events
SET published_at = $2, last_error = ''
WHERE id = $1 AND published_at IS NULL`,
		id, at.UTC(),
	)
	if err != nil {
		return fmt.Errorf("postgres: mark outbox event published: %w", err)
	}
	return nil
}

// MarkFailed records a delivery failure and schedules the next attempt.
func (r *OutboxRepository) MarkFailed(ctx context.Context, id, cause string, next time.Time) error {
	cause = strings.TrimSpace(cause)
	if len(cause) > 500 {
		cause = cause[:500]
	}

	_, err := querierFrom(ctx, r.db).ExecContext(ctx, `
UPDATE outbox_events
SET attempts = attempts + 1, last_error = $2, next_attempt_at = $3
WHERE id = $1 AND published_at IS NULL`,
		id, cause, next.UTC(),
	)
	if err != nil {
		return fmt.Errorf("postgres: mark outbox event failed: %w", err)
	}
	return nil
}

// Prune deletes published events older than before.
func (r *OutboxRepository) Prune(ctx context.Context, before time.Time) (int64, error) {
	result, err := querierFrom(ctx, r.db).ExecContext(ctx, `
DELETE FROM outbox_events
WHERE published_at IS NOT NULL AND published_at < $1`,
		before.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("postgres: prune outbox events: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("postgres: prune outbox rows affected: %w", err)
	}
	return removed, nil
}
