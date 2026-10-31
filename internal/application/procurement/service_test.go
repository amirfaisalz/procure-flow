package procurement_test

import (
	"context"
	"errors"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/domain"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

var fixedNow = time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)

type fakeIDs struct {
	id     string
	number string
}

func (f fakeIDs) NewID() string { return f.id }

func (f fakeIDs) NewNumber(time.Time) string { return f.number }

type fakeRepo struct {
	item domainproc.Procurement
	has  bool

	createErr error
	getErr    error
	updateErr error
	listErr   error

	createCalled bool
	getCalled    bool
	updateCalled bool
	listCalled   bool

	updateExpected int
	lastFilter     domainproc.ListFilter
	listResult     domainproc.ListResult
}

func (f *fakeRepo) Create(_ context.Context, p domainproc.Procurement) error {
	f.createCalled = true
	if f.createErr != nil {
		return f.createErr
	}
	f.item = p
	f.has = true
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, id string) (domainproc.Procurement, error) {
	f.getCalled = true
	if f.getErr != nil {
		return domainproc.Procurement{}, f.getErr
	}
	if !f.has || f.item.ID != id {
		return domainproc.Procurement{}, domain.ErrNotFound
	}
	return f.item, nil
}

func (f *fakeRepo) Update(_ context.Context, p domainproc.Procurement, expectedVersion int) error {
	f.updateCalled = true
	f.updateExpected = expectedVersion
	if f.updateErr != nil {
		return f.updateErr
	}
	f.item = p
	f.has = true
	return nil
}

func (f *fakeRepo) List(_ context.Context, filter domainproc.ListFilter) (domainproc.ListResult, error) {
	f.listCalled = true
	f.lastFilter = filter
	if f.listErr != nil {
		return domainproc.ListResult{}, f.listErr
	}
	return f.listResult, nil
}

func newService(repo *fakeRepo) *app.Service {
	return app.NewService(repo, fakeIDs{id: "prc_1", number: "PR-202610-0001"}, func() time.Time {
		return fixedNow
	})
}

func validInput() app.CreateInput {
	return app.CreateInput{
		Title:       "Laptop procurement for engineering",
		Description: "20 units of development laptops",
		Department:  "Engineering",
		Requester:   "Amir",
		Amount:      250_000_000,
		Currency:    "idr",
	}
}

func requireFieldError(t *testing.T, err error, field string) {
	t.Helper()

	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want *domain.ValidationError", err)
	}
	if _, ok := validationErr.Fields[field]; !ok {
		t.Fatalf("validation fields = %v, want field %q", validationErr.Fields, field)
	}
}

func TestServiceCreate(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(repo)

	got, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if !repo.createCalled {
		t.Fatal("repository Create was not called")
	}
	if got.ID != "prc_1" || got.Number != "PR-202610-0001" {
		t.Errorf("ID/Number = %q/%q, want generated values", got.ID, got.Number)
	}
	if got.Status != domainproc.StatusDraft {
		t.Errorf("Status = %q, want draft", got.Status)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if got.Currency != "IDR" {
		t.Errorf("Currency = %q, want IDR", got.Currency)
	}
	if !got.CreatedAt.Equal(fixedNow) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, fixedNow)
	}
	if repo.item.ID != got.ID {
		t.Errorf("repository item ID = %q, want %q", repo.item.ID, got.ID)
	}
}

func TestServiceCreateValidation(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(repo)

	input := validInput()
	input.Title = ""
	_, err := svc.Create(context.Background(), input)

	requireFieldError(t, err, "title")
	if repo.createCalled {
		t.Error("repository Create was called for invalid input")
	}
}

func TestServiceCreateRepositoryError(t *testing.T) {
	repo := &fakeRepo{createErr: domain.ErrConflict}
	svc := newService(repo)

	_, err := svc.Create(context.Background(), validInput())
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create() error = %v, want ErrConflict", err)
	}
}

func TestServiceGet(t *testing.T) {
	repo := &fakeRepo{has: true, item: domainproc.Procurement{ID: "prc_1", Title: "Laptop"}}
	svc := newService(repo)

	got, err := svc.Get(context.Background(), " prc_1 ")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != "prc_1" {
		t.Errorf("ID = %q, want prc_1", got.ID)
	}

	_, err = svc.Get(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrNotFound", err)
	}
}

func TestServiceGetRejectsEmptyID(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(repo)

	_, err := svc.Get(context.Background(), "   ")
	requireFieldError(t, err, "id")
	if repo.getCalled {
		t.Error("repository GetByID was called for an empty ID")
	}
}

func TestServiceListDefaultsAndNormalization(t *testing.T) {
	repo := &fakeRepo{
		listResult: domainproc.ListResult{
			Items:    []domainproc.Procurement{{ID: "prc_1"}},
			Total:    1,
			Page:     1,
			PageSize: domainproc.DefaultPageSize,
		},
	}
	svc := newService(repo)

	got, err := svc.List(context.Background(), domainproc.ListFilter{Query: "  laptop  "})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !repo.listCalled {
		t.Fatal("repository List was not called")
	}
	if repo.lastFilter.Page != 1 {
		t.Errorf("filter.Page = %d, want 1", repo.lastFilter.Page)
	}
	if repo.lastFilter.PageSize != domainproc.DefaultPageSize {
		t.Errorf("filter.PageSize = %d, want %d", repo.lastFilter.PageSize, domainproc.DefaultPageSize)
	}
	if repo.lastFilter.Query != "laptop" {
		t.Errorf("filter.Query = %q, want %q", repo.lastFilter.Query, "laptop")
	}
	if got.Total != 1 {
		t.Errorf("Total = %d, want 1", got.Total)
	}
}

func TestServiceListValidation(t *testing.T) {
	tests := []struct {
		name   string
		filter domainproc.ListFilter
		field  string
	}{
		{
			name:   "negative page",
			filter: domainproc.ListFilter{Page: -1},
			field:  "page",
		},
		{
			name:   "negative page size",
			filter: domainproc.ListFilter{PageSize: -1},
			field:  "page_size",
		},
		{
			name:   "page size above maximum",
			filter: domainproc.ListFilter{PageSize: domainproc.MaxPageSize + 1},
			field:  "page_size",
		},
		{
			name:   "unknown status",
			filter: domainproc.ListFilter{Status: domainproc.Status("unknown")},
			field:  "status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := newService(repo)

			_, err := svc.List(context.Background(), tt.filter)
			requireFieldError(t, err, tt.field)
			if repo.listCalled {
				t.Error("repository List was called for an invalid filter")
			}
		})
	}
}

func TestServiceSubmit(t *testing.T) {
	repo := &fakeRepo{
		has:  true,
		item: domainproc.Procurement{ID: "prc_1", Status: domainproc.StatusDraft, Version: 1},
	}
	svc := newService(repo)

	got, err := svc.Submit(context.Background(), "prc_1")
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if got.Status != domainproc.StatusSubmitted {
		t.Errorf("Status = %q, want submitted", got.Status)
	}
	if got.Version != 2 {
		t.Errorf("Version = %d, want 2", got.Version)
	}
	if !repo.updateCalled {
		t.Fatal("repository Update was not called")
	}
	if repo.updateExpected != 1 {
		t.Errorf("expectedVersion = %d, want 1", repo.updateExpected)
	}
}

func TestServiceApproveFromDraft(t *testing.T) {
	repo := &fakeRepo{
		has:  true,
		item: domainproc.Procurement{ID: "prc_1", Status: domainproc.StatusDraft, Version: 1},
	}
	svc := newService(repo)

	_, err := svc.Approve(context.Background(), "prc_1")
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("Approve() error = %v, want ErrInvalidTransition", err)
	}
	if repo.updateCalled {
		t.Error("repository Update was called after a failed transition")
	}
}

func TestServiceReject(t *testing.T) {
	repo := &fakeRepo{
		has:  true,
		item: domainproc.Procurement{ID: "prc_1", Status: domainproc.StatusSubmitted, Version: 2},
	}
	svc := newService(repo)

	got, err := svc.Reject(context.Background(), "prc_1", "budget exceeded")
	if err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	if got.Status != domainproc.StatusRejected {
		t.Errorf("Status = %q, want rejected", got.Status)
	}
	if got.RejectionReason != "budget exceeded" {
		t.Errorf("RejectionReason = %q, want %q", got.RejectionReason, "budget exceeded")
	}
	if repo.updateExpected != 2 {
		t.Errorf("expectedVersion = %d, want 2", repo.updateExpected)
	}
}

func TestServiceRejectRequiresReason(t *testing.T) {
	repo := &fakeRepo{
		has:  true,
		item: domainproc.Procurement{ID: "prc_1", Status: domainproc.StatusSubmitted, Version: 2},
	}
	svc := newService(repo)

	_, err := svc.Reject(context.Background(), "prc_1", "   ")
	requireFieldError(t, err, "reason")
	if repo.updateCalled {
		t.Error("repository Update was called with an invalid reason")
	}
}

func TestServiceUpdateConflict(t *testing.T) {
	repo := &fakeRepo{
		has:       true,
		item:      domainproc.Procurement{ID: "prc_1", Status: domainproc.StatusDraft, Version: 1},
		updateErr: domain.ErrConflict,
	}
	svc := newService(repo)

	_, err := svc.Submit(context.Background(), "prc_1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Submit() error = %v, want ErrConflict", err)
	}
}

func TestServiceListRepositoryError(t *testing.T) {
	repo := &fakeRepo{listErr: errors.New("boom")}
	svc := newService(repo)

	_, err := svc.List(context.Background(), domainproc.ListFilter{})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("List() error = %v, want boom", err)
	}
}
