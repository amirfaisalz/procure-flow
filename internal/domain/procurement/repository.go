package procurement

import (
	"context"
)

const (
	// DefaultPageSize is used when the caller does not request a page size.
	DefaultPageSize = 20
	// MaxPageSize bounds repository and API response sizes.
	MaxPageSize = 100
)

// ListFilter describes a paginated, optionally filtered query.
type ListFilter struct {
	Status   Status
	Query    string
	Page     int
	PageSize int
}

// ListResult is a single page of procurement records.
type ListResult struct {
	Items    []Procurement
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

// Repository is the persistence port for the procurement aggregate.
//
// Update must fail with domain.ErrConflict when the stored version differs from
// expectedVersion. Implementations must return copies, never internal pointers.
type Repository interface {
	Create(ctx context.Context, p Procurement) error
	GetByID(ctx context.Context, id string) (Procurement, error)
	Update(ctx context.Context, p Procurement, expectedVersion int) error
	List(ctx context.Context, filter ListFilter) (ListResult, error)
}
