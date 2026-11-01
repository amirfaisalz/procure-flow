package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

type fakeAcknowledger struct {
	acks    int
	nacks   int
	requeue bool
}

func (f *fakeAcknowledger) Ack(uint64, bool) error { f.acks++; return nil }

func (f *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	f.nacks++
	f.requeue = requeue
	return nil
}

func (f *fakeAcknowledger) Reject(tag uint64, requeue bool) error {
	return f.Nack(tag, false, requeue)
}

func testEvent() event.Event {
	return event.Event{
		ID:            "evt_1",
		Type:          "procurement.approved",
		AggregateType: "procurement",
		AggregateID:   "prc_1",
		ActorID:       "user_fin",
		OccurredAt:    time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC),
		Payload:       []byte(`{"id":"prc_1","number":"PR-1","title":"Laptops","status":"approved"}`),
	}
}

func testConsumer(handler Handler, attempts int) *Consumer {
	cfg := DefaultConsumerConfig("test.queue")
	cfg.MaxAttempts = attempts
	cfg.RetryDelay = time.Millisecond
	return NewConsumer("amqp://unused", "procure.events", cfg, handler, slog.New(slog.DiscardHandler))
}

func delivery(t *testing.T, ev event.Event, ack *fakeAcknowledger) amqp.Delivery {
	t.Helper()
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	return amqp.Delivery{Acknowledger: ack, DeliveryTag: 1, Body: body}
}

func TestHandleAcksSuccessfulEvents(t *testing.T) {
	var calls int
	consumer := testConsumer(func(context.Context, event.Event) error {
		calls++
		return nil
	}, 3)

	ack := &fakeAcknowledger{}
	consumer.handle(context.Background(), delivery(t, testEvent(), ack))

	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
	if ack.acks != 1 || ack.nacks != 0 {
		t.Errorf("acks/nacks = %d/%d, want 1/0", ack.acks, ack.nacks)
	}
}

func TestHandleRetriesThenDeadLetters(t *testing.T) {
	var calls int
	consumer := testConsumer(func(context.Context, event.Event) error {
		calls++
		return errors.New("downstream unavailable")
	}, 3)

	ack := &fakeAcknowledger{}
	consumer.handle(context.Background(), delivery(t, testEvent(), ack))

	if calls != 3 {
		t.Errorf("handler calls = %d, want 3 attempts", calls)
	}
	if ack.nacks != 1 {
		t.Errorf("nacks = %d, want 1", ack.nacks)
	}
	if ack.requeue {
		t.Error("failed event was requeued; want dead-lettering")
	}
}

func TestHandleRejectsUndecodablePayload(t *testing.T) {
	var calls int
	consumer := testConsumer(func(context.Context, event.Event) error {
		calls++
		return nil
	}, 1)

	ack := &fakeAcknowledger{}
	consumer.handle(context.Background(), amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  1,
		Body:         []byte("{not json"),
	})

	if calls != 0 {
		t.Errorf("handler calls = %d, want 0", calls)
	}
	if ack.nacks != 1 {
		t.Errorf("nacks = %d, want 1", ack.nacks)
	}
}
