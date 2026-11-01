package tracing_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/platform/tracing"
)

var (
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

func TestNewProducesValidIDs(t *testing.T) {
	ids := tracing.New()
	if !traceIDPattern.MatchString(ids.TraceID) {
		t.Errorf("TraceID = %q, want 32 hex characters", ids.TraceID)
	}
	if !spanIDPattern.MatchString(ids.SpanID) {
		t.Errorf("SpanID = %q, want 16 hex characters", ids.SpanID)
	}
	if ids.TraceID == ids.SpanID {
		t.Error("TraceID and SpanID must differ")
	}
}

func TestNewSpanKeepsTraceID(t *testing.T) {
	ids := tracing.New()
	child := ids.NewSpan()
	if child.TraceID != ids.TraceID {
		t.Errorf("child TraceID = %q, want %q", child.TraceID, ids.TraceID)
	}
	if child.SpanID == ids.SpanID {
		t.Error("child span reused the parent span identifier")
	}
}

func TestTraceparentRoundTrip(t *testing.T) {
	ids := tracing.New()
	parsed, ok := tracing.ParseTraceparent(ids.Traceparent())
	if !ok {
		t.Fatal("ParseTraceparent() rejected a value produced by Traceparent()")
	}
	if parsed != ids {
		t.Errorf("parsed = %+v, want %+v", parsed, ids)
	}
}

func TestParseTraceparent(t *testing.T) {
	valid := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ids, ok := tracing.ParseTraceparent(valid)
	if !ok {
		t.Fatal("ParseTraceparent() rejected a valid header")
	}
	if ids.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || ids.SpanID != "00f067aa0ba902b7" {
		t.Errorf("ids = %+v", ids)
	}

	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "too few parts", value: "00-abc-def"},
		{name: "bad version", value: "zz-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		{name: "version ff", value: "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		{name: "short trace", value: "00-4bf92f3577b34da6a3ce929d0e0e47-00f067aa0ba902b7-01"},
		{name: "zero trace", value: "00-00000000000000000000000000000000-00f067aa0ba902b7-01"},
		{name: "zero span", value: "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01"},
		{name: "bad flags", value: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-zz"},
		{name: "non hex", value: "00-4bf92f3577b34da6a3ce929d0e0e473g-00f067aa0ba902b7-01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := tracing.ParseTraceparent(tt.value); ok {
				t.Errorf("ParseTraceparent(%q) = ok, want rejection", tt.value)
			}
		})
	}
}

func TestContextRoundTrip(t *testing.T) {
	ids := tracing.New()
	ctx := tracing.WithContext(context.Background(), ids)

	got, ok := tracing.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext() = false, want true")
	}
	if got != ids {
		t.Errorf("FromContext() = %+v, want %+v", got, ids)
	}

	if _, ok := tracing.FromContext(context.Background()); ok {
		t.Error("FromContext() on an empty context = true, want false")
	}
}
