// Package event defines the envelope for domain events delivered through the
// transactional outbox to the message broker.
package event

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Event is one immutable domain event. Payload is the JSON-encoded body
// consumed by workers; the envelope fields drive routing and idempotency.
type Event struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	ActorID       string          `json:"actor_id,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
	// Attempts is populated by the outbox when the event is claimed; it is the
	// number of failed delivery attempts so far.
	Attempts int `json:"-"`
}

// Validate checks the envelope invariants required by the broker and store.
func (e Event) Validate() error {
	switch {
	case strings.TrimSpace(e.ID) == "":
		return fmt.Errorf("event: id must not be empty")
	case strings.TrimSpace(e.Type) == "":
		return fmt.Errorf("event: type must not be empty")
	case strings.TrimSpace(e.AggregateType) == "":
		return fmt.Errorf("event: aggregate type must not be empty")
	case strings.TrimSpace(e.AggregateID) == "":
		return fmt.Errorf("event: aggregate id must not be empty")
	case e.OccurredAt.IsZero():
		return fmt.Errorf("event: occurred_at must be set")
	case len(e.Payload) == 0:
		return fmt.Errorf("event: payload must not be empty")
	case !json.Valid(e.Payload):
		return fmt.Errorf("event: payload must be valid JSON")
	}
	return nil
}

// NewID returns a unique event identifier such as evt_9f2c... . The random
// source falls back to a counter-based hash if the OS entropy source fails.
func NewID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err == nil {
		return "evt_" + hex.EncodeToString(buf)
	}

	sum := sha256.Sum256([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), fallbackCounter.Add(1))))
	return "evt_" + hex.EncodeToString(sum[:16])
}

var fallbackCounter atomic.Uint64
