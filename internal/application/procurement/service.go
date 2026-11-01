package procurement

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
	"github.com/amirfaisalz/procure-flow/internal/domain/outbox"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

// Audit actions recorded for procurement lifecycle events.
const (
	ActionCreated   = "procurement.created"
	ActionSubmitted = "procurement.submitted"
	ActionApproved  = "procurement.approved"
	ActionRejected  = "procurement.rejected"
	ActionCancelled = "procurement.cancelled"
)

// createAttempts bounds retries when a generated request number or identifier
// collides with an existing row. Collisions are already negligible, so a small
// budget is enough to absorb a rare clash without surfacing a 409 to clients.
const createAttempts = 3

// IDGenerator supplies identifiers for new aggregates.
type IDGenerator interface {
	NewID() string
	NewNumber(now time.Time) string
}

// UnitOfWork runs a function inside a single storage transaction. When it is
// nil, writes execute directly; this keeps the in-memory adapter usable while
// PostgreSQL commits the aggregate and its audit events atomically.
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(context.Context) error) error
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

// Option customises the service.
type Option func(*Service)

// WithAudit attaches an audit repository and, optionally, a unit of work.
func WithAudit(repo audit.Repository, uow UnitOfWork) Option {
	return func(s *Service) {
		s.audit = repo
		s.uow = uow
	}
}

// WithOutbox attaches the transactional outbox used to publish lifecycle
// events to the broker.
func WithOutbox(repo outbox.Repository) Option {
	return func(s *Service) {
		s.outbox = repo
	}
}

// WithCache attaches a read-through aggregate cache. A non-positive TTL falls
// back to the default; a nil cache disables caching.
func WithCache(cache Cache, ttl time.Duration) Option {
	return func(s *Service) {
		s.cache = cache
		if ttl <= 0 {
			ttl = defaultCacheTTL
		}
		s.cacheTTL = ttl
	}
}

// Service implements procurement use cases.
type Service struct {
	repo     domainproc.Repository
	audit    audit.Repository
	outbox   outbox.Repository
	uow      UnitOfWork
	cache    Cache
	cacheTTL time.Duration
	ids      IDGenerator
	now      func() time.Time
}

// NewService wires the use-case service. A nil clock defaults to time.Now.
func NewService(repo domainproc.Repository, ids IDGenerator, now func() time.Time, opts ...Option) *Service {
	if now == nil {
		now = time.Now
	}
	service := &Service{repo: repo, ids: ids, now: now}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

// Create validates and persists a new draft procurement. Ownership is always
// derived from the authenticated actor; the optional requester name is only a
// display label and cannot change requester_id.
func (s *Service) Create(ctx context.Context, input CreateInput) (domainproc.Procurement, error) {
	actor, err := requireActor(ctx)
	if err != nil {
		return domainproc.Procurement{}, err
	}
	if !actor.IsAdmin() && !actor.HasRole(RoleRequester) {
		return domainproc.Procurement{}, domain.ErrForbidden
	}

	requester := strings.TrimSpace(input.Requester)
	if requester == "" {
		requester = actor.DisplayName()
	}

	var p domainproc.Procurement
	for attempt := 0; attempt < createAttempts; attempt++ {
		now := s.now().UTC()
		p, err = domainproc.New(
			s.ids.NewID(),
			s.ids.NewNumber(now),
			domainproc.CreateParams{
				Title:       input.Title,
				Description: input.Description,
				Department:  input.Department,
				Requester:   requester,
				RequesterID: actor.Subject,
				Amount:      input.Amount,
				Currency:    input.Currency,
			},
			now,
		)
		if err != nil {
			return domainproc.Procurement{}, err
		}

		err = s.withinTx(ctx, func(ctx context.Context) error {
			if err := s.repo.Create(ctx, p); err != nil {
				return err
			}
			if err := s.record(ctx, actor, p, ActionCreated, ""); err != nil {
				return err
			}
			return s.emit(ctx, actor, p, EventCreated, "")
		})
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, domain.ErrConflict) {
			return domainproc.Procurement{}, err
		}
	}
	return domainproc.Procurement{}, err
}

// Get returns one procurement by ID. Reads are served from the cache when one
// is configured; the actor is still required on every call.
func (s *Service) Get(ctx context.Context, id string) (domainproc.Procurement, error) {
	if _, err := requireActor(ctx); err != nil {
		return domainproc.Procurement{}, err
	}
	id, err := normalizeID(id)
	if err != nil {
		return domainproc.Procurement{}, err
	}

	key := procurementCacheKey(id)
	var generation string
	cacheable := false
	if s.cache != nil {
		var cached domainproc.Procurement
		found, gen, cacheErr := s.cache.Get(ctx, key, &cached)
		if cacheErr == nil {
			if found {
				return cached, nil
			}
			// Remember the generation observed before the database read. A
			// mutation that commits in the meantime advances it, and the
			// conditional set below then refuses to cache the stale value.
			generation, cacheable = gen, true
		}
	}

	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domainproc.Procurement{}, err
	}
	if cacheable {
		_, _ = s.cache.SetIfGeneration(ctx, key, p, s.cacheTTL, generation)
	}
	return p, nil
}

// get loads the aggregate directly from storage. Mutations call this inside a
// transaction and must always observe committed state, so it bypasses the cache.
func (s *Service) get(ctx context.Context, id string) (domainproc.Procurement, error) {
	id, err := normalizeID(id)
	if err != nil {
		return domainproc.Procurement{}, err
	}
	return s.repo.GetByID(ctx, id)
}

// List returns a validated, paginated page of procurements.
func (s *Service) List(ctx context.Context, filter domainproc.ListFilter) (domainproc.ListResult, error) {
	if _, err := requireActor(ctx); err != nil {
		return domainproc.ListResult{}, err
	}

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
	return s.mutate(ctx, id, ActionSubmitted, canSubmit, func(p *domainproc.Procurement, now time.Time) error {
		return p.Submit(now)
	})
}

// Approve accepts a submitted procurement.
func (s *Service) Approve(ctx context.Context, id string) (domainproc.Procurement, error) {
	return s.mutate(ctx, id, ActionApproved, canApprove, func(p *domainproc.Procurement, now time.Time) error {
		return p.Approve(now)
	})
}

// Reject declines a submitted procurement.
func (s *Service) Reject(ctx context.Context, id, reason string) (domainproc.Procurement, error) {
	return s.mutate(ctx, id, ActionRejected, canApprove, func(p *domainproc.Procurement, now time.Time) error {
		return p.Reject(now, reason)
	})
}

// Cancel closes a draft or submitted procurement.
func (s *Service) Cancel(ctx context.Context, id string) (domainproc.Procurement, error) {
	return s.mutate(ctx, id, ActionCancelled, canCancel, func(p *domainproc.Procurement, now time.Time) error {
		return p.Cancel(now)
	})
}

// Audit returns a page of the audit trail for one procurement, newest first.
func (s *Service) Audit(ctx context.Context, id string, filter audit.ListFilter) (audit.ListResult, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return audit.ListResult{}, err
	}

	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.Page < 0 {
		return audit.ListResult{}, &domain.ValidationError{
			Fields: map[string]string{"page": "must be greater than zero"},
		}
	}
	if filter.PageSize == 0 {
		filter.PageSize = audit.DefaultPageSize
	}
	if filter.PageSize < 0 || filter.PageSize > audit.MaxPageSize {
		return audit.ListResult{}, &domain.ValidationError{
			Fields: map[string]string{"page_size": "must be between 1 and 100"},
		}
	}

	filter.EntityType = audit.EntityProcurement
	filter.EntityID = strings.TrimSpace(id)
	if s.audit == nil {
		return audit.ListResult{
			Events:   []audit.Event{},
			Page:     filter.Page,
			PageSize: filter.PageSize,
		}, nil
	}
	return s.audit.List(ctx, filter)
}

func (s *Service) mutate(
	ctx context.Context,
	id string,
	action string,
	allow func(Actor, domainproc.Procurement) bool,
	apply func(*domainproc.Procurement, time.Time) error,
) (domainproc.Procurement, error) {
	actor, err := requireActor(ctx)
	if err != nil {
		return domainproc.Procurement{}, err
	}

	var result domainproc.Procurement
	err = s.withinTx(ctx, func(ctx context.Context) error {
		p, err := s.get(ctx, id)
		if err != nil {
			return err
		}
		if !allow(actor, p) {
			return domain.ErrForbidden
		}

		fromStatus := string(p.Status)
		expectedVersion := p.Version
		if err := apply(&p, s.now().UTC()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, p, expectedVersion); err != nil {
			return err
		}
		if err := s.record(ctx, actor, p, action, fromStatus); err != nil {
			return err
		}
		if err := s.emit(ctx, actor, p, action, fromStatus); err != nil {
			return err
		}
		result = p
		return nil
	})
	if err != nil {
		return domainproc.Procurement{}, err
	}
	s.invalidate(ctx, result.ID)
	return result, nil
}

func (s *Service) invalidate(ctx context.Context, id string) {
	if s.cache == nil {
		return
	}
	_ = s.cache.Delete(ctx, procurementCacheKey(id))
}

func (s *Service) withinTx(ctx context.Context, fn func(context.Context) error) error {
	if s.uow == nil {
		return fn(ctx)
	}
	return s.uow.WithinTx(ctx, fn)
}

func (s *Service) record(ctx context.Context, actor Actor, p domainproc.Procurement, action, fromStatus string) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Append(ctx, audit.Event{
		EntityType: audit.EntityProcurement,
		EntityID:   p.ID,
		Action:     action,
		ActorID:    actor.Subject,
		ActorName:  actor.DisplayName(),
		ActorRole:  primaryRole(actor),
		FromStatus: fromStatus,
		ToStatus:   string(p.Status),
		Metadata:   map[string]string{"number": p.Number},
		OccurredAt: s.now().UTC(),
	})
}

// requireActor fails closed when no authenticated actor is present.
func requireActor(ctx context.Context) (Actor, error) {
	actor, ok := ActorFrom(ctx)
	if !ok {
		return Actor{}, domain.ErrForbidden
	}
	return actor, nil
}

func canSubmit(actor Actor, p domainproc.Procurement) bool {
	if actor.IsAdmin() {
		return true
	}
	if !actor.HasRole(RoleRequester) {
		return false
	}
	// Requests without a recorded owner (imported or legacy rows) cannot be
	// submitted by an arbitrary requester; only an admin may act on them.
	return p.RequesterID != "" && p.RequesterID == actor.Subject
}

func canApprove(actor Actor, p domainproc.Procurement) bool {
	if actor.IsAdmin() {
		return true
	}
	if !actor.HasRole(RoleApprover) {
		return false
	}
	// Segregation of duties: a non-admin approver cannot approve a request they
	// created themselves, even when the department matches.
	if p.RequesterID != "" && p.RequesterID == actor.Subject {
		return false
	}
	department := strings.TrimSpace(actor.Department)
	return department != "" && strings.EqualFold(department, strings.TrimSpace(p.Department))
}

func canCancel(actor Actor, p domainproc.Procurement) bool {
	if actor.IsAdmin() {
		return true
	}
	if !actor.HasRole(RoleRequester) {
		return false
	}
	return p.RequesterID != "" && p.RequesterID == actor.Subject
}

func primaryRole(actor Actor) string {
	switch {
	case actor.IsAdmin():
		return RoleAdmin
	case actor.HasRole(RoleApprover):
		return RoleApprover
	case actor.HasRole(RoleRequester):
		return RoleRequester
	case actor.HasRole(RoleViewer):
		return RoleViewer
	default:
		return ""
	}
}
