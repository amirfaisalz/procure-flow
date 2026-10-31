package id_test

import (
	"encoding/hex"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/platform/id"
)

func TestGeneratorNewID(t *testing.T) {
	gen := id.NewGenerator()
	seen := make(map[string]struct{}, 100)

	for i := 0; i < 100; i++ {
		got := gen.NewID()
		if !strings.HasPrefix(got, "prc_") {
			t.Fatalf("NewID() = %q, want prc_ prefix", got)
		}
		raw := strings.TrimPrefix(got, "prc_")
		if len(raw) != 32 {
			t.Fatalf("len(NewID()) = %d, want 36 (id=%q)", len(got), got)
		}
		if _, err := hex.DecodeString(raw); err != nil {
			t.Fatalf("NewID() = %q is not hex: %v", got, err)
		}
		if _, exists := seen[got]; exists {
			t.Fatalf("NewID() returned duplicate id %q", got)
		}
		seen[got] = struct{}{}
	}
}

func TestGeneratorNewNumber(t *testing.T) {
	gen := id.NewGenerator()
	pattern := regexp.MustCompile(`^PR-\d{6}-[A-F0-9]{8}$`)

	now := time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)
	got := gen.NewNumber(now)
	if !pattern.MatchString(got) {
		t.Fatalf("NewNumber() = %q, want format PR-YYYYMM-XXXXXXXX", got)
	}
	if !strings.HasPrefix(got, "PR-202610-") {
		t.Errorf("NewNumber() = %q, want PR-202610- prefix", got)
	}
}
