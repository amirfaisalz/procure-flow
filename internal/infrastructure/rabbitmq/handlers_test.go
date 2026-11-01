package rabbitmq

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

func notificationEvent() event.Event {
	return event.Event{
		ID:            "evt_1",
		Type:          "procurement.approved",
		AggregateType: "procurement",
		AggregateID:   "prc_1",
		ActorID:       "user_fin",
		OccurredAt:    time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC),
		Payload: []byte(`{
			"id": "prc_1",
			"number": "PR-202610-0001",
			"title": "Laptops",
			"department": "Engineering",
			"requester": "Amir",
			"requester_id": "user_amir",
			"amount": 250000000,
			"currency": "IDR",
			"status": "approved"
		}`),
	}
}

func TestNotificationHandlerPostsWebhook(t *testing.T) {
	var got notification
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", contentType)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	handler := NotificationHandler(slog.New(slog.DiscardHandler), server.URL, server.Client())
	if err := handler(context.Background(), notificationEvent()); err != nil {
		t.Fatalf("handler error = %v", err)
	}

	if got.EventID != "evt_1" || got.Recipient != "Amir" || got.RecipientID != "user_amir" {
		t.Errorf("notification = %+v, want the requester and event identity", got)
	}
	if got.Message != "Procurement PR-202610-0001 (Laptops) was approved." {
		t.Errorf("message = %q", got.Message)
	}
}

func TestNotificationHandlerFailsOnWebhookError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	handler := NotificationHandler(nil, server.URL, server.Client())
	if err := handler(context.Background(), notificationEvent()); err == nil {
		t.Fatal("handler error = nil, want the webhook failure")
	}
}

func TestNotificationHandlerWithoutWebhookLogs(t *testing.T) {
	handler := NotificationHandler(slog.New(slog.DiscardHandler), "", nil)
	if err := handler(context.Background(), notificationEvent()); err != nil {
		t.Fatalf("handler error = %v, want nil", err)
	}
}

func TestNotificationHandlerRejectsBadPayload(t *testing.T) {
	ev := notificationEvent()
	ev.Payload = []byte(`"not an object"`)

	handler := NotificationHandler(slog.New(slog.DiscardHandler), "", nil)
	if err := handler(context.Background(), ev); err == nil {
		t.Fatal("handler error = nil, want a payload error")
	}
}

func TestERPHandlerPostsEnvelope(t *testing.T) {
	var got event.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	handler := ERPHandler(slog.New(slog.DiscardHandler), server.URL, server.Client())
	if err := handler(context.Background(), notificationEvent()); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if got.ID != "evt_1" || got.Type != "procurement.approved" {
		t.Errorf("ERP payload = %+v, want the event envelope", got)
	}
}

func TestERPHandlerWithoutWebhookIsNoop(t *testing.T) {
	handler := ERPHandler(slog.New(slog.DiscardHandler), "", nil)
	if err := handler(context.Background(), notificationEvent()); err != nil {
		t.Fatalf("handler error = %v, want nil", err)
	}
}
