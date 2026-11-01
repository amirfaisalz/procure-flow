// Package audit defines the append-only audit trail contract.
package audit

import (
	"context"
	"time"
)

// EntityProcurement is the entity type for procurement events.
const EntityProcurement = "procurement"

const (
	// DefaultPageSize is used when the caller does not request a page size.
	DefaultPageSize = 20
	// MaxPageSize bounds repository and API response sizes.
	MaxPageSize = 100
)

// Event is one immutable audit record.
type Event struct {
	ID         int64             `json:"id"`
	EntityType string            `json:"entity_type"`
	EntityID   string            `json:"entity_id"`
	Action     string            `json:"action"`
	ActorID    string            `json:"actor_id"`
	ActorName  string            `json:"actor_name"`
	ActorRole  string            `json:"actor_role"`
	FromStatus string            `json:"from_status,omitempty"`
	ToStatus   string            `json:"to_status,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	OccurredAt time.Time         `json:"occurred_at"`
}

// ListFilter selects one page of events for a single entity.
type ListFilter struct {
	EntityType string
	EntityID   string
	Page       int
	PageSize   int
}

// ListResult is a single page of audit events, newest first.
type ListResult struct {
	Events   []Event
	Total    int
	Page     int
	PageSize int
}

// TotalPages returns the number of pages for the result set.
func (r ListResult) TotalPages() int {
	if r.Total == 0 || r.PageSize <= 0 {
		return 0
	}
	return (r.Total + r.PageSize - 1) / r.PageSize
}

// Repository is the append-only persistence port for audit events.
type Repository interface {
	Append(ctx context.Context, event Event) error
	List(ctx context.Context, filter ListFilter) (ListResult, error)
}
