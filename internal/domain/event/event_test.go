package event_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

func validEvent() event.Event {
	return event.Event{
		ID:            "evt_1",
		Type:          "procurement.created",
		AggregateType: "procurement",
		AggregateID:   "prc_1",
		OccurredAt:    time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC),
		Payload:       json.RawMessage(`{"id":"prc_1"}`),
	}
}

func TestValidate(t *testing.T) {
	if err := validEvent().Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}

	tests := []struct {
		name   string
		mutate func(*event.Event)
	}{
		{name: "missing id", mutate: func(e *event.Event) { e.ID = "" }},
		{name: "missing type", mutate: func(e *event.Event) { e.Type = "" }},
		{name: "missing aggregate type", mutate: func(e *event.Event) { e.AggregateType = "" }},
		{name: "missing aggregate id", mutate: func(e *event.Event) { e.AggregateID = "" }},
		{name: "missing occurred at", mutate: func(e *event.Event) { e.OccurredAt = time.Time{} }},
		{name: "empty payload", mutate: func(e *event.Event) { e.Payload = nil }},
		{name: "invalid payload", mutate: func(e *event.Event) { e.Payload = json.RawMessage(`{`) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := validEvent()
			tt.mutate(&ev)
			if err := ev.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want an error")
			}
		})
	}
}

func TestNewIDIsUniqueAndPrefixed(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := event.NewID()
		if !strings.HasPrefix(id, "evt_") {
			t.Fatalf("NewID() = %q, want an evt_ prefix", id)
		}
		if len(id) != len("evt_")+32 {
			t.Fatalf("NewID() = %q, want 16 random bytes as hex", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("NewID() returned a duplicate %q", id)
		}
		seen[id] = struct{}{}
	}
}
