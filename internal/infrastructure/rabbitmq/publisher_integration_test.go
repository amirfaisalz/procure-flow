package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

func TestPublisherRoundTrip(t *testing.T) {
	url := os.Getenv("TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("TEST_RABBITMQ_URL is not set; skipping RabbitMQ integration test")
	}

	exchange := fmt.Sprintf("procure.test.%d", time.Now().UnixNano())
	pub := NewPublisher(url, exchange)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pub.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = pub.Close() }()

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial for consumer: %v", err)
	}
	defer func() { _ = conn.Close() }()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("open consumer channel: %v", err)
	}
	defer func() { _ = channel.Close() }()
	defer func() { _ = channel.ExchangeDelete(exchange, false, false) }()

	queue, err := channel.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("declare queue: %v", err)
	}
	if err := channel.QueueBind(queue.Name, "procurement.approved", exchange, false, nil); err != nil {
		t.Fatalf("bind queue: %v", err)
	}
	deliveries, err := channel.Consume(queue.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}

	want := event.Event{
		ID:            "evt_integration",
		Type:          "procurement.approved",
		AggregateType: "procurement",
		AggregateID:   "prc_integration",
		ActorID:       "user_fin",
		OccurredAt:    time.Now().UTC().Truncate(time.Second),
		Payload:       json.RawMessage(`{"id":"prc_integration","status":"approved"}`),
	}
	if err := pub.Publish(ctx, want); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := pub.Healthy(ctx); err != nil {
		t.Fatalf("Healthy() error = %v", err)
	}

	select {
	case delivery := <-deliveries:
		if delivery.MessageId != want.ID {
			t.Errorf("MessageId = %q, want %q", delivery.MessageId, want.ID)
		}
		if delivery.Type != want.Type {
			t.Errorf("Type = %q, want %q", delivery.Type, want.Type)
		}
		if delivery.ContentType != "application/json" {
			t.Errorf("ContentType = %q, want application/json", delivery.ContentType)
		}
		if delivery.DeliveryMode != amqp.Persistent {
			t.Errorf("DeliveryMode = %d, want persistent", delivery.DeliveryMode)
		}
		if got := delivery.Headers["aggregate_id"]; got != want.AggregateID {
			t.Errorf("aggregate_id header = %v, want %q", got, want.AggregateID)
		}

		var got event.Event
		if err := json.Unmarshal(delivery.Body, &got); err != nil {
			t.Fatalf("decode event body: %v", err)
		}
		if got.ID != want.ID || string(got.Payload) != `{"id":"prc_integration","status":"approved"}` {
			t.Errorf("body = %+v, want the event envelope", got)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the published event")
	}
}
