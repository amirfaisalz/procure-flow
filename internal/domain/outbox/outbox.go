// Package outbox defines the transactional outbox port. Events are written in
// the same transaction as the aggregate change and delivered asynchronously.
package outbox

import (
	"context"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

// Repository stores pending events and tracks delivery attempts.
//
// Claim must be safe to call from several API replicas at once: an event is
// handed to at most one relay for the duration of the lease, and becomes
// claimable again if the relay crashes before marking an outcome.
type Repository interface {
	// Enqueue persists an event as part of the caller's transaction.
	Enqueue(ctx context.Context, ev event.Event) error
	// Claim leases up to limit due, unpublished events until now+lease.
	Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]event.Event, error)
	// MarkPublished records successful delivery to the broker.
	MarkPublished(ctx context.Context, id string, at time.Time) error
	// MarkFailed records a delivery failure and schedules the next attempt.
	MarkFailed(ctx context.Context, id string, cause string, next time.Time) error
	// Prune deletes published events older than before and reports how many.
	Prune(ctx context.Context, before time.Time) (int64, error)
}
