package procurement_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/domain/event"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

type fakeOutbox struct {
	events     []event.Event
	enqueueErr error
}

func (f *fakeOutbox) Enqueue(_ context.Context, ev event.Event) error {
	if f.enqueueErr != nil {
		return f.enqueueErr
	}
	f.events = append(f.events, ev)
	return nil
}

func (f *fakeOutbox) Claim(context.Context, int, time.Time, time.Duration) ([]event.Event, error) {
	return nil, nil
}

func (f *fakeOutbox) MarkPublished(context.Context, string, time.Time) error { return nil }

func (f *fakeOutbox) MarkFailed(context.Context, string, string, time.Time) error { return nil }

func (f *fakeOutbox) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

type decodedPayload struct {
	ID              string `json:"id"`
	Number          string `json:"number"`
	Status          string `json:"status"`
	FromStatus      string `json:"from_status"`
	RejectionReason string `json:"rejection_reason"`
	Actor           struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"actor"`
}

func decodePayload(t *testing.T, ev event.Event) decodedPayload {
	t.Helper()
	var payload decodedPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	return payload
}

func serviceWithOutbox(repo *fakeRepo, outbox *fakeOutbox) *app.Service {
	return app.NewService(repo, fakeIDs{id: "prc_1", number: "PR-202610-0001"}, func() time.Time {
		return fixedNow
	}, app.WithOutbox(outbox))
}

func TestServiceCreateEmitsCreatedEvent(t *testing.T) {
	repo := &fakeRepo{}
	outbox := &fakeOutbox{}
	svc := serviceWithOutbox(repo, outbox)

	if _, err := svc.Create(adminCtx(), validInput()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(outbox.events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(outbox.events))
	}

	ev := outbox.events[0]
	if ev.Type != app.EventCreated {
		t.Errorf("Type = %q, want %q", ev.Type, app.EventCreated)
	}
	if ev.AggregateType != "procurement" || ev.AggregateID != "prc_1" {
		t.Errorf("aggregate = %s/%s, want procurement/prc_1", ev.AggregateType, ev.AggregateID)
	}
	if ev.ID == "" || !ev.OccurredAt.Equal(fixedNow) {
		t.Errorf("envelope = %+v, want a generated ID and the service clock", ev)
	}

	payload := decodePayload(t, ev)
	if payload.ID != "prc_1" || payload.Status != "draft" || payload.Number != "PR-202610-0001" {
		t.Errorf("payload = %+v, want the created draft", payload)
	}
	if payload.FromStatus != "" {
		t.Errorf("FromStatus = %q, want empty for creation", payload.FromStatus)
	}
	if payload.Actor.ID != "user_admin" || payload.Actor.Role != app.RoleAdmin {
		t.Errorf("actor = %+v, want the authenticated admin", payload.Actor)
	}
}

func TestServiceSubmitEmitsSubmittedEvent(t *testing.T) {
	repo := &fakeRepo{item: sampleProcurement(), has: true}
	outbox := &fakeOutbox{}
	svc := serviceWithOutbox(repo, outbox)

	if _, err := svc.Submit(adminCtx(), "prc_1"); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if len(outbox.events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(outbox.events))
	}
	payload := decodePayload(t, outbox.events[0])
	if payload.Status != "submitted" || payload.FromStatus != "draft" {
		t.Errorf("payload status = %s from %s, want submitted from draft", payload.Status, payload.FromStatus)
	}
}

func TestServiceApproveEmitsApprovedEvent(t *testing.T) {
	item := sampleProcurement()
	item.Status = domainproc.StatusSubmitted
	repo := &fakeRepo{item: item, has: true}
	outbox := &fakeOutbox{}
	svc := serviceWithOutbox(repo, outbox)

	if _, err := svc.Approve(adminCtx(), "prc_1"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	payload := decodePayload(t, outbox.events[0])
	if payload.Status != "approved" || payload.FromStatus != "submitted" {
		t.Errorf("payload status = %s from %s, want approved from submitted", payload.Status, payload.FromStatus)
	}
}

func TestServiceRejectEmitsReason(t *testing.T) {
	item := sampleProcurement()
	item.Status = domainproc.StatusSubmitted
	repo := &fakeRepo{item: item, has: true}
	outbox := &fakeOutbox{}
	svc := serviceWithOutbox(repo, outbox)

	if _, err := svc.Reject(adminCtx(), "prc_1", "budget exhausted"); err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	payload := decodePayload(t, outbox.events[0])
	if payload.Status != "rejected" || payload.RejectionReason != "budget exhausted" {
		t.Errorf("payload = %+v, want rejected with the reason", payload)
	}
}

func TestServiceFailedTransitionEmitsNoEvent(t *testing.T) {
	item := sampleProcurement()
	item.Status = domainproc.StatusApproved
	repo := &fakeRepo{item: item, has: true}
	outbox := &fakeOutbox{}
	svc := serviceWithOutbox(repo, outbox)

	if _, err := svc.Cancel(adminCtx(), "prc_1"); err == nil {
		t.Fatal("Cancel() error = nil, want an invalid-transition error")
	}
	if len(outbox.events) != 0 {
		t.Errorf("emitted %d events for a failed transition, want 0", len(outbox.events))
	}
}

func TestServiceEventEnqueueFailureFailsTheTransaction(t *testing.T) {
	repo := &fakeRepo{}
	outbox := &fakeOutbox{enqueueErr: errors.New("outbox unavailable")}
	svc := serviceWithOutbox(repo, outbox)

	if _, err := svc.Create(adminCtx(), validInput()); err == nil {
		t.Fatal("Create() error = nil, want the outbox failure")
	}
}
