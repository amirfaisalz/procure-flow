package domain_test

import (
	"strings"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain"
)

func TestValidationError(t *testing.T) {
	err := &domain.ValidationError{
		Fields: map[string]string{
			"title":  "is required",
			"amount": "must be greater than zero",
		},
	}

	msg := err.Error()
	for _, want := range []string{"amount", "must be greater than zero", "title", "is required"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, want it to contain %q", msg, want)
		}
	}
	if strings.Index(msg, "amount") > strings.Index(msg, "title") {
		t.Errorf("Error() = %q, want fields sorted alphabetically", msg)
	}
}

func TestValidationErrorOrNil(t *testing.T) {
	if err := (&domain.ValidationError{}).OrNil(); err != nil {
		t.Errorf("OrNil() with no fields = %v, want nil", err)
	}

	err := (&domain.ValidationError{Fields: map[string]string{"title": "is required"}}).OrNil()
	if err == nil {
		t.Fatal("OrNil() with fields = nil, want error")
	}
}

func TestValidationErrorAdd(t *testing.T) {
	err := &domain.ValidationError{}
	err.Add("title", "is required")
	err.Add("amount", "must be greater than zero")

	if len(err.Fields) != 2 {
		t.Fatalf("len(Fields) = %d, want 2", len(err.Fields))
	}
	if err.Fields["title"] != "is required" {
		t.Errorf("Fields[title] = %q, want %q", err.Fields["title"], "is required")
	}
}
