package requestid_test

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
)

func TestNewProducesUniqueHexIDs(t *testing.T) {
	seen := make(map[string]struct{}, 100)

	for i := 0; i < 100; i++ {
		id := requestid.New()
		if len(id) != 32 {
			t.Fatalf("len(New()) = %d, want 32 (id=%q)", len(id), id)
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("New() = %q is not hex: %v", id, err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("New() returned duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := requestid.With(context.Background(), "req-123")

	if got := requestid.From(ctx); got != "req-123" {
		t.Errorf("From(ctx) = %q, want %q", got, "req-123")
	}
}

func TestFromEmptyContext(t *testing.T) {
	if got := requestid.From(context.Background()); got != "" {
		t.Errorf("From(background) = %q, want empty string", got)
	}
}
