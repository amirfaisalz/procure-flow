package procurement

import (
	"context"
	"strings"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

// IDGenerator supplies identifiers for new aggregates.
type IDGenerator interface {
	NewID() string
	NewNumber(now time.Time) string
}

// CreateInput is the application-level command for creating a procurement.
type CreateInput struct {
	Title       string
	Description string
	Department  string
	Requester   string
	Amount      int64
	Currency    string
}

// Service implements procurement use cases.
type Service struct {
	repo domainproc.Repository
	ids  IDGenerator
	now  func() time.Time
}

// NewService wires the use-case service. A nil clock defaults to time.Now.
func NewService(repo domainproc.Repository, ids IDGenerator, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, ids: ids, now: now}
}

// Create validates and persists a new draft procurement.
func (s *Service) Create(ctx context.Context, input CreateInput) (domainproc.Procurement, error) {
	now := s.now().UTC()
	p, err := domainproc.New(
		s.ids.NewID(),
		s.ids.NewNumber(now),
		domainproc.CreateParams{
			Title:       input.Title,
			Description: input.Description,
			Department:  input.Department,
			Requester:   input.Requester,
			Amount:      input.Amount,
			Currency:    input.Currency,
		},
		now,
	)
	if err != nil {
		return domainproc.Procurement{}, err
	}

	if err := s.repo.Create(ctx, p); err != nil {
		return domainproc.Procurement{}, err
	}
	return p, nil
}

// Get returns one procurement by ID.
func (s *Service) Get(ctx context.Context, id string) (domainproc.Procurement, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return domainproc.Procurement{}, &domain.ValidationError{
			Fields: map[string]string{"id": "must not be empty"},
		}
	}
	return s.repo.GetByID(ctx, id)
}

// List returns a validated, paginated page of procurements.
func (s *Service) List(ctx context.Context, filter domainproc.ListFilter) (domainproc.ListResult, error) {
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.Page < 0 {
		return domainproc.ListResult{}, &domain.ValidationError{
			Fields: map[string]string{"page": "must be greater than zero"},
		}
	}

	if filter.PageSize == 0 {
		filter.PageSize = domainproc.DefaultPageSize
	}
	if filter.PageSize < 0 || filter.PageSize > domainproc.MaxPageSize {
		return domainproc.ListResult{}, &domain.ValidationError{
			Fields: map[string]string{"page_size": "must be between 1 and 100"},
		}
	}

	if filter.Status != "" && !filter.Status.Valid() {
		return domainproc.ListResult{}, &domain.ValidationError{
			Fields: map[string]string{"status": "unknown status"},
		}
	}

	filter.Query = strings.TrimSpace(filter.Query)
	return s.repo.List(ctx, filter)
}

// Submit moves a draft procurement into the approval queue.
func (s *Service) Submit(ctx context.Context, id string) (domainproc.Procurement, error) {
	return s.mutate(ctx, id, func(p *domainproc.Procurement, now time.Time) error {
		return p.Submit(now)
	})
}

// Approve accepts a submitted procurement.
func (s *Service) Approve(ctx context.Context, id string) (domainproc.Procurement, error) {
	return s.mutate(ctx, id, func(p *domainproc.Procurement, now time.Time) error {
		return p.Approve(now)
	})
}

// Reject declines a submitted procurement.
func (s *Service) Reject(ctx context.Context, id, reason string) (domainproc.Procurement, error) {
	return s.mutate(ctx, id, func(p *domainproc.Procurement, now time.Time) error {
		return p.Reject(now, reason)
	})
}

// Cancel closes a draft or submitted procurement.
func (s *Service) Cancel(ctx context.Context, id string) (domainproc.Procurement, error) {
	return s.mutate(ctx, id, func(p *domainproc.Procurement, now time.Time) error {
		return p.Cancel(now)
	})
}

func (s *Service) mutate(
	ctx context.Context,
	id string,
	apply func(*domainproc.Procurement, time.Time) error,
) (domainproc.Procurement, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return domainproc.Procurement{}, err
	}

	expectedVersion := p.Version
	if err := apply(&p, s.now().UTC()); err != nil {
		return domainproc.Procurement{}, err
	}

	if err := s.repo.Update(ctx, p, expectedVersion); err != nil {
		return domainproc.Procurement{}, err
	}
	return p, nil
}
