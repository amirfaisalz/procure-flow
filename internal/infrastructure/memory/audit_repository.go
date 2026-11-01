package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
)

const (
	defaultAuditLimit = 200
	maxAuditLimit     = 500
)

// AuditRepository is a thread-safe, in-memory append-only audit store used for
// local development and tests.
type AuditRepository struct {
	mu     sync.RWMutex
	nextID int64
	events map[string][]audit.Event
}

// NewAuditRepository creates an empty audit repository.
func NewAuditRepository() *AuditRepository {
	return &AuditRepository{events: make(map[string][]audit.Event)}
}

// Append stores an event and assigns its sequential identifier.
func (r *AuditRepository) Append(ctx context.Context, event audit.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	event.ID = r.nextID
	event.Metadata = cloneMetadata(event.Metadata)

	key := auditKey(event.EntityType, event.EntityID)
	r.events[key] = append(r.events[key], event)
	return nil
}

// ListByEntity returns the newest events for one entity, up to limit.
func (r *AuditRepository) ListByEntity(ctx context.Context, entityType, entityID string, limit int) ([]audit.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultAuditLimit
	}
	if limit > maxAuditLimit {
		limit = maxAuditLimit
	}

	r.mu.RLock()
	stored := r.events[auditKey(entityType, entityID)]
	out := make([]audit.Event, len(stored))
	for i, event := range stored {
		event.Metadata = cloneMetadata(event.Metadata)
		out[i] = event
	}
	r.mu.RUnlock()

	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func auditKey(entityType, entityID string) string {
	return entityType + "\x00" + entityID
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	copied := make(map[string]string, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}
