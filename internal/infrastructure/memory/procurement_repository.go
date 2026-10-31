package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

// ProcurementRepository is a thread-safe, in-memory adapter used for local
// development and tests. Replace it with a SQL adapter in production without
// changing the application layer.
type ProcurementRepository struct {
	mu    sync.RWMutex
	items map[string]procurement.Procurement
}

// NewProcurementRepository creates an empty repository.
func NewProcurementRepository() *ProcurementRepository {
	return &ProcurementRepository{
		items: make(map[string]procurement.Procurement),
	}
}

// Ping reports whether the repository is reachable.
func (r *ProcurementRepository) Ping(ctx context.Context) error {
	return ctx.Err()
}

// Create stores a new procurement.
func (r *ProcurementRepository) Create(ctx context.Context, p procurement.Procurement) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.items[p.ID]; exists {
		return domain.ErrConflict
	}
	r.items[p.ID] = p
	return nil
}

// GetByID returns a copy of a stored procurement.
func (r *ProcurementRepository) GetByID(ctx context.Context, id string) (procurement.Procurement, error) {
	if err := ctx.Err(); err != nil {
		return procurement.Procurement{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.items[id]
	if !ok {
		return procurement.Procurement{}, domain.ErrNotFound
	}
	return p, nil
}

// Update replaces a procurement when expectedVersion matches the stored version.
func (r *ProcurementRepository) Update(ctx context.Context, p procurement.Procurement, expectedVersion int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.items[p.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if stored.Version != expectedVersion {
		return domain.ErrConflict
	}
	r.items[p.ID] = p
	return nil
}

// List returns a filtered page ordered by newest first, then ID.
func (r *ProcurementRepository) List(ctx context.Context, filter procurement.ListFilter) (procurement.ListResult, error) {
	if err := ctx.Err(); err != nil {
		return procurement.ListResult{}, err
	}

	r.mu.RLock()
	matched := make([]procurement.Procurement, 0, len(r.items))
	for _, p := range r.items {
		if filter.Status != "" && p.Status != filter.Status {
			continue
		}
		if !matchesQuery(p, filter.Query) {
			continue
		}
		matched = append(matched, p)
	}
	r.mu.RUnlock()

	sort.SliceStable(matched, func(i, j int) bool {
		if !matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].CreatedAt.After(matched[j].CreatedAt)
		}
		return matched[i].ID < matched[j].ID
	})

	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = procurement.DefaultPageSize
	}
	if pageSize > procurement.MaxPageSize {
		pageSize = procurement.MaxPageSize
	}

	total := len(matched)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	items := make([]procurement.Procurement, end-start)
	copy(items, matched[start:end])

	return procurement.ListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func matchesQuery(p procurement.Procurement, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}

	query = strings.ToLower(query)
	for _, value := range []string{p.Number, p.Title, p.Description, p.Department, p.Requester} {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
