package procurement

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

// Event types published to the broker. They mirror the audit actions so
// operations and consumers reason about the same lifecycle vocabulary.
const (
	EventCreated   = ActionCreated
	EventSubmitted = ActionSubmitted
	EventApproved  = ActionApproved
	EventRejected  = ActionRejected
	EventCancelled = ActionCancelled
)

// eventPayload is the JSON body of a procurement lifecycle event.
type eventPayload struct {
	ID              string     `json:"id"`
	Number          string     `json:"number"`
	Title           string     `json:"title"`
	Department      string     `json:"department"`
	Requester       string     `json:"requester"`
	RequesterID     string     `json:"requester_id"`
	Amount          int64      `json:"amount"`
	Currency        string     `json:"currency"`
	Status          string     `json:"status"`
	FromStatus      string     `json:"from_status,omitempty"`
	RejectionReason string     `json:"rejection_reason,omitempty"`
	Version         int        `json:"version"`
	Actor           eventActor `json:"actor"`
	OccurredAt      time.Time  `json:"occurred_at"`
}

type eventActor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// emit records one lifecycle event in the outbox. It runs inside the caller's
// transaction so the event and the aggregate change commit atomically.
func (s *Service) emit(ctx context.Context, actor Actor, p domainproc.Procurement, eventType, fromStatus string) error {
	if s.outbox == nil {
		return nil
	}

	now := s.now().UTC()
	payload, err := json.Marshal(eventPayload{
		ID:              p.ID,
		Number:          p.Number,
		Title:           p.Title,
		Department:      p.Department,
		Requester:       p.Requester,
		RequesterID:     p.RequesterID,
		Amount:          p.Amount,
		Currency:        p.Currency,
		Status:          string(p.Status),
		FromStatus:      fromStatus,
		RejectionReason: p.RejectionReason,
		Version:         p.Version,
		Actor: eventActor{
			ID:   actor.Subject,
			Name: actor.DisplayName(),
			Role: primaryRole(actor),
		},
		OccurredAt: now,
	})
	if err != nil {
		return fmt.Errorf("procurement: encode event payload: %w", err)
	}

	return s.outbox.Enqueue(ctx, event.Event{
		ID:            event.NewID(),
		Type:          eventType,
		AggregateType: "procurement",
		AggregateID:   p.ID,
		ActorID:       actor.Subject,
		OccurredAt:    now,
		Payload:       payload,
	})
}
