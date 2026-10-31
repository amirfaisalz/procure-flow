package procurement_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

var fixedNow = time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)

func validParams() procurement.CreateParams {
	return procurement.CreateParams{
		Title:       "Laptop procurement for engineering",
		Description: "20 units of development laptops",
		Department:  "Engineering",
		Requester:   "Amir",
		Amount:      250_000_000,
		Currency:    "idr",
	}
}

func mustNew(t *testing.T, params procurement.CreateParams) procurement.Procurement {
	t.Helper()

	p, err := procurement.New("prc_1", "PR-202610-0001", params, fixedNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return p
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

func TestNewProcurement(t *testing.T) {
	t.Run("creates a valid draft", func(t *testing.T) {
		p := mustNew(t, validParams())

		if p.ID != "prc_1" {
			t.Errorf("ID = %q, want %q", p.ID, "prc_1")
		}
		if p.Number != "PR-202610-0001" {
			t.Errorf("Number = %q, want %q", p.Number, "PR-202610-0001")
		}
		if p.Status != procurement.StatusDraft {
			t.Errorf("Status = %q, want %q", p.Status, procurement.StatusDraft)
		}
		if p.Version != 1 {
			t.Errorf("Version = %d, want 1", p.Version)
		}
		if !p.CreatedAt.Equal(fixedNow) {
			t.Errorf("CreatedAt = %v, want %v", p.CreatedAt, fixedNow)
		}
		if !p.UpdatedAt.Equal(fixedNow) {
			t.Errorf("UpdatedAt = %v, want %v", p.UpdatedAt, fixedNow)
		}
	})

	t.Run("normalizes user input and timestamps", func(t *testing.T) {
		params := validParams()
		params.Title = "  Laptop procurement  "
		params.Description = "  20 units  "
		params.Department = "  Engineering  "
		params.Requester = "  Amir  "
		params.Currency = " idr "

		now := time.Date(2026, time.October, 31, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
		p, err := procurement.New(" prc_1 ", " PR-202610-0001 ", params, now)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		if p.ID != "prc_1" || p.Number != "PR-202610-0001" {
			t.Errorf("ID/Number not trimmed: %q / %q", p.ID, p.Number)
		}
		if p.Title != "Laptop procurement" {
			t.Errorf("Title = %q, want trimmed title", p.Title)
		}
		if p.Description != "20 units" {
			t.Errorf("Description = %q, want trimmed description", p.Description)
		}
		if p.Department != "Engineering" || p.Requester != "Amir" {
			t.Errorf("Department/Requester not trimmed: %q / %q", p.Department, p.Requester)
		}
		if p.Currency != "IDR" {
			t.Errorf("Currency = %q, want %q", p.Currency, "IDR")
		}
		if p.CreatedAt.Location() != time.UTC {
			t.Errorf("CreatedAt location = %v, want UTC", p.CreatedAt.Location())
		}
	})

	t.Run("requires internal identifiers", func(t *testing.T) {
		_, err := procurement.New("", "", validParams(), fixedNow)
		requireFieldError(t, err, "id")
		requireFieldError(t, err, "number")
	})
}

func TestNewProcurementValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*procurement.CreateParams)
		field  string
	}{
		{
			name:   "empty title",
			mutate: func(p *procurement.CreateParams) { p.Title = "" },
			field:  "title",
		},
		{
			name:   "title too short",
			mutate: func(p *procurement.CreateParams) { p.Title = "ab" },
			field:  "title",
		},
		{
			name:   "title too long",
			mutate: func(p *procurement.CreateParams) { p.Title = strings.Repeat("a", 201) },
			field:  "title",
		},
		{
			name:   "empty department",
			mutate: func(p *procurement.CreateParams) { p.Department = "   " },
			field:  "department",
		},
		{
			name:   "empty requester",
			mutate: func(p *procurement.CreateParams) { p.Requester = "" },
			field:  "requester",
		},
		{
			name:   "zero amount",
			mutate: func(p *procurement.CreateParams) { p.Amount = 0 },
			field:  "amount",
		},
		{
			name:   "negative amount",
			mutate: func(p *procurement.CreateParams) { p.Amount = -1 },
			field:  "amount",
		},
		{
			name:   "short currency",
			mutate: func(p *procurement.CreateParams) { p.Currency = "ID" },
			field:  "currency",
		},
		{
			name:   "currency with invalid characters",
			mutate: func(p *procurement.CreateParams) { p.Currency = "IDR1" },
			field:  "currency",
		},
		{
			name:   "description too long",
			mutate: func(p *procurement.CreateParams) { p.Description = strings.Repeat("a", 2001) },
			field:  "description",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := validParams()
			tt.mutate(&params)

			_, err := procurement.New("prc_1", "PR-202610-0001", params, fixedNow)
			requireFieldError(t, err, tt.field)
		})
	}
}

func TestProcurementValidateRejectsUnknownStatus(t *testing.T) {
	p := mustNew(t, validParams())
	p.Status = procurement.Status("unknown")

	requireFieldError(t, p.Validate(), "status")
}

func TestSubmit(t *testing.T) {
	p := mustNew(t, validParams())
	later := fixedNow.Add(time.Hour)

	if err := p.Submit(later); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if p.Status != procurement.StatusSubmitted {
		t.Errorf("Status = %q, want %q", p.Status, procurement.StatusSubmitted)
	}
	if p.Version != 2 {
		t.Errorf("Version = %d, want 2", p.Version)
	}
	if !p.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", p.UpdatedAt, later)
	}
	if !p.CreatedAt.Equal(fixedNow) {
		t.Errorf("CreatedAt changed to %v, want %v", p.CreatedAt, fixedNow)
	}

	if err := p.Submit(later); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("second Submit() error = %v, want ErrInvalidTransition", err)
	}
	if p.Version != 2 {
		t.Errorf("Version after failed transition = %d, want 2", p.Version)
	}
}

func TestApprove(t *testing.T) {
	t.Run("from submitted", func(t *testing.T) {
		p := mustNew(t, validParams())
		if err := p.Submit(fixedNow); err != nil {
			t.Fatalf("Submit() error = %v", err)
		}

		later := fixedNow.Add(time.Hour)
		if err := p.Approve(later); err != nil {
			t.Fatalf("Approve() error = %v", err)
		}
		if p.Status != procurement.StatusApproved {
			t.Errorf("Status = %q, want %q", p.Status, procurement.StatusApproved)
		}
		if p.Version != 3 {
			t.Errorf("Version = %d, want 3", p.Version)
		}
	})

	t.Run("from draft is rejected", func(t *testing.T) {
		p := mustNew(t, validParams())

		if err := p.Approve(fixedNow); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("Approve() error = %v, want ErrInvalidTransition", err)
		}
		if p.Status != procurement.StatusDraft || p.Version != 1 {
			t.Errorf("state changed after failed transition: status=%q version=%d", p.Status, p.Version)
		}
	})
}

func TestReject(t *testing.T) {
	t.Run("requires a reason", func(t *testing.T) {
		p := mustNew(t, validParams())
		if err := p.Submit(fixedNow); err != nil {
			t.Fatalf("Submit() error = %v", err)
		}

		err := p.Reject(fixedNow, "   ")
		requireFieldError(t, err, "reason")
		if p.Status != procurement.StatusSubmitted || p.Version != 2 {
			t.Errorf("state changed after invalid reject: status=%q version=%d", p.Status, p.Version)
		}
	})

	t.Run("from submitted stores a trimmed reason", func(t *testing.T) {
		p := mustNew(t, validParams())
		if err := p.Submit(fixedNow); err != nil {
			t.Fatalf("Submit() error = %v", err)
		}

		later := fixedNow.Add(time.Hour)
		if err := p.Reject(later, "  budget exceeded  "); err != nil {
			t.Fatalf("Reject() error = %v", err)
		}
		if p.Status != procurement.StatusRejected {
			t.Errorf("Status = %q, want %q", p.Status, procurement.StatusRejected)
		}
		if p.RejectionReason != "budget exceeded" {
			t.Errorf("RejectionReason = %q, want %q", p.RejectionReason, "budget exceeded")
		}
		if p.Version != 3 {
			t.Errorf("Version = %d, want 3", p.Version)
		}
	})

	t.Run("from draft is rejected", func(t *testing.T) {
		p := mustNew(t, validParams())

		if err := p.Reject(fixedNow, "budget exceeded"); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("Reject() error = %v, want ErrInvalidTransition", err)
		}
	})
}

func TestCancel(t *testing.T) {
	t.Run("from draft", func(t *testing.T) {
		p := mustNew(t, validParams())

		if err := p.Cancel(fixedNow); err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		if p.Status != procurement.StatusCancelled {
			t.Errorf("Status = %q, want %q", p.Status, procurement.StatusCancelled)
		}
	})

	t.Run("from submitted", func(t *testing.T) {
		p := mustNew(t, validParams())
		if err := p.Submit(fixedNow); err != nil {
			t.Fatalf("Submit() error = %v", err)
		}

		if err := p.Cancel(fixedNow); err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		if p.Status != procurement.StatusCancelled {
			t.Errorf("Status = %q, want %q", p.Status, procurement.StatusCancelled)
		}
	})

	t.Run("from approved is rejected", func(t *testing.T) {
		p := mustNew(t, validParams())
		if err := p.Submit(fixedNow); err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		if err := p.Approve(fixedNow); err != nil {
			t.Fatalf("Approve() error = %v", err)
		}

		if err := p.Cancel(fixedNow); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("Cancel() error = %v, want ErrInvalidTransition", err)
		}
	})
}

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from procurement.Status
		to   procurement.Status
		want bool
	}{
		{procurement.StatusDraft, procurement.StatusSubmitted, true},
		{procurement.StatusDraft, procurement.StatusCancelled, true},
		{procurement.StatusDraft, procurement.StatusApproved, false},
		{procurement.StatusSubmitted, procurement.StatusApproved, true},
		{procurement.StatusSubmitted, procurement.StatusRejected, true},
		{procurement.StatusSubmitted, procurement.StatusCancelled, true},
		{procurement.StatusSubmitted, procurement.StatusSubmitted, false},
		{procurement.StatusApproved, procurement.StatusCancelled, false},
		{procurement.StatusRejected, procurement.StatusSubmitted, false},
		{procurement.StatusCancelled, procurement.StatusDraft, false},
		{procurement.Status("unknown"), procurement.StatusDraft, false},
	}

	p := procurement.Procurement{}
	for _, tt := range tests {
		p.Status = tt.from
		if got := p.CanTransition(tt.to); got != tt.want {
			t.Errorf("CanTransition(%q -> %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestStatusValid(t *testing.T) {
	valid := []procurement.Status{
		procurement.StatusDraft,
		procurement.StatusSubmitted,
		procurement.StatusApproved,
		procurement.StatusRejected,
		procurement.StatusCancelled,
	}
	for _, status := range valid {
		if !status.Valid() {
			t.Errorf("%q.Valid() = false, want true", status)
		}
	}

	for _, status := range []procurement.Status{"", "unknown", "DRAFT"} {
		if status.Valid() {
			t.Errorf("%q.Valid() = true, want false", status)
		}
	}
}
