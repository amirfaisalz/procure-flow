// Package audit defines the append-only audit trail contract.
package audit

import (
	"context"
	"time"
)

// EntityProcurement is the entity type for procurement events.
const EntityProcurement = "procurement"

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

// Repository is the append-only persistence port for audit events.
type Repository interface {
	Append(ctx context.Context, event Event) error
	ListByEntity(ctx context.Context, entityType, entityID string, limit int) ([]Event, error)
}
