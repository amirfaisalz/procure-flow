package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
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

// List returns a page of events for one entity, newest first.
func (r *AuditRepository) List(ctx context.Context, filter audit.ListFilter) (audit.ListResult, error) {
	if err := ctx.Err(); err != nil {
		return audit.ListResult{}, err
	}

	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = audit.DefaultPageSize
	}
	if pageSize > audit.MaxPageSize {
		pageSize = audit.MaxPageSize
	}

	r.mu.RLock()
	stored := r.events[auditKey(filter.EntityType, filter.EntityID)]
	out := make([]audit.Event, len(stored))
	for i, event := range stored {
		event.Metadata = cloneMetadata(event.Metadata)
		out[i] = event
	}
	r.mu.RUnlock()

	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].OccurredAt.Equal(out[j].OccurredAt) {
			return out[i].OccurredAt.After(out[j].OccurredAt)
		}
		return out[i].ID > out[j].ID
	})

	total := len(out)
	start := pageStart(page, pageSize, total)
	end := start + pageSize
	if end > total {
		end = total
	}

	pageEvents := make([]audit.Event, end-start)
	copy(pageEvents, out[start:end])

	return audit.ListResult{
		Events:   pageEvents,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
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
