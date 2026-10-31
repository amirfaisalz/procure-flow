package procurement

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amirfaisalz/procure-flow/internal/domain"
)

// Status represents the lifecycle state of a procurement request.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusSubmitted Status = "submitted"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusSubmitted, StatusApproved, StatusRejected, StatusCancelled:
		return true
	default:
		return false
	}
}

// Procurement is the aggregate root for a purchase request.
//
// Amount is stored in the minor unit of Currency (for example cents) to avoid
// floating-point rounding errors. Version implements optimistic concurrency.
type Procurement struct {
	ID              string
	Number          string
	Title           string
	Description     string
	Department      string
	Requester       string
	RequesterID     string
	Amount          int64
	Currency        string
	Status          Status
	RejectionReason string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int
}

// CreateParams contains the caller-supplied fields for a new procurement.
type CreateParams struct {
	Title       string
	Description string
	Department  string
	Requester   string
	RequesterID string
	Amount      int64
	Currency    string
}

// New builds a valid procurement in draft status.
func New(id, number string, params CreateParams, now time.Time) (Procurement, error) {
	now = now.UTC()
	p := Procurement{
		ID:          strings.TrimSpace(id),
		Number:      strings.TrimSpace(number),
		Title:       strings.TrimSpace(params.Title),
		Description: strings.TrimSpace(params.Description),
		Department:  strings.TrimSpace(params.Department),
		Requester:   strings.TrimSpace(params.Requester),
		RequesterID: strings.TrimSpace(params.RequesterID),
		Amount:      params.Amount,
		Currency:    strings.ToUpper(strings.TrimSpace(params.Currency)),
		Status:      StatusDraft,
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     1,
	}
	if err := p.Validate(); err != nil {
		return Procurement{}, err
	}
	return p, nil
}

// Validate checks all invariants of the aggregate.
func (p Procurement) Validate() error {
	errs := &domain.ValidationError{}

	if p.ID == "" {
		errs.Add("id", "must not be empty")
	}
	if p.Number == "" {
		errs.Add("number", "must not be empty")
	}
	if n := utf8.RuneCountInString(p.Title); n < 3 || n > 200 {
		errs.Add("title", "must be between 3 and 200 characters")
	}
	if utf8.RuneCountInString(p.Description) > 2000 {
		errs.Add("description", "must be at most 2000 characters")
	}
	if p.Department == "" {
		errs.Add("department", "must not be empty")
	} else if utf8.RuneCountInString(p.Department) > 100 {
		errs.Add("department", "must be at most 100 characters")
	}
	if p.Requester == "" {
		errs.Add("requester", "must not be empty")
	} else if utf8.RuneCountInString(p.Requester) > 100 {
		errs.Add("requester", "must be at most 100 characters")
	}
	if p.Amount <= 0 {
		errs.Add("amount", "must be greater than zero")
	}
	if !validCurrency(p.Currency) {
		errs.Add("currency", "must be a three-letter ISO 4217 code")
	}
	if p.Status == "" {
		errs.Add("status", "must not be empty")
	} else if !p.Status.Valid() {
		errs.Add("status", fmt.Sprintf("unknown status %q", p.Status))
	}

	return errs.OrNil()
}

func validCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for i := 0; i < len(currency); i++ {
		if currency[i] < 'A' || currency[i] > 'Z' {
			return false
		}
	}
	return true
}

// CanTransition reports whether the aggregate may move to the target status.
func (p Procurement) CanTransition(to Status) bool {
	switch p.Status {
	case StatusDraft:
		return to == StatusSubmitted || to == StatusCancelled
	case StatusSubmitted:
		return to == StatusApproved || to == StatusRejected || to == StatusCancelled
	default:
		return false
	}
}

// Submit moves a draft procurement into the approval queue.
func (p *Procurement) Submit(now time.Time) error {
	if !p.CanTransition(StatusSubmitted) {
		return invalidTransition(p.Status, StatusSubmitted)
	}
	p.Status = StatusSubmitted
	p.touch(now)
	return nil
}

// Approve accepts a submitted procurement.
func (p *Procurement) Approve(now time.Time) error {
	if !p.CanTransition(StatusApproved) {
		return invalidTransition(p.Status, StatusApproved)
	}
	p.Status = StatusApproved
	p.RejectionReason = ""
	p.touch(now)
	return nil
}

// Reject declines a submitted procurement and records the reason.
func (p *Procurement) Reject(now time.Time, reason string) error {
	if !p.CanTransition(StatusRejected) {
		return invalidTransition(p.Status, StatusRejected)
	}

	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		return &domain.ValidationError{
			Fields: map[string]string{"reason": "must be between 3 and 500 characters"},
		}
	}

	p.Status = StatusRejected
	p.RejectionReason = reason
	p.touch(now)
	return nil
}

// Cancel closes a draft or submitted procurement.
func (p *Procurement) Cancel(now time.Time) error {
	if !p.CanTransition(StatusCancelled) {
		return invalidTransition(p.Status, StatusCancelled)
	}
	p.Status = StatusCancelled
	p.touch(now)
	return nil
}

func (p *Procurement) touch(now time.Time) {
	p.UpdatedAt = now.UTC()
	p.Version++
}

func invalidTransition(from, to Status) error {
	return fmt.Errorf("%w: cannot transition from %q to %q", domain.ErrInvalidTransition, from, to)
}
