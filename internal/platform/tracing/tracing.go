// Package tracing implements W3C Trace Context propagation without an
// external tracing SDK. It produces the identifiers needed to correlate logs
// across services; exporting spans to a backend remains a future integration.
package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// IDs identifies one trace and the current span within it.
type IDs struct {
	TraceID string
	SpanID  string
}

type contextKey struct{}

// New returns a fresh trace and span identifier.
func New() IDs {
	return IDs{TraceID: randomHex(16), SpanID: randomHex(8)}
}

// NewSpan keeps the trace identifier and creates a new span identifier.
func (ids IDs) NewSpan() IDs {
	return IDs{TraceID: ids.TraceID, SpanID: randomHex(8)}
}

// Traceparent renders the W3C traceparent header value.
func (ids IDs) Traceparent() string {
	return "00-" + ids.TraceID + "-" + ids.SpanID + "-01"
}

// ParseTraceparent parses a W3C traceparent header value.
func ParseTraceparent(value string) (IDs, bool) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 {
		return IDs{}, false
	}

	version, traceID, spanID, flags := parts[0], parts[1], parts[2], parts[3]
	if len(version) != 2 || !isHex(version) || version == "ff" {
		return IDs{}, false
	}
	if len(traceID) != 32 || !isHex(traceID) || allZero(traceID) {
		return IDs{}, false
	}
	if len(spanID) != 16 || !isHex(spanID) || allZero(spanID) {
		return IDs{}, false
	}
	if len(flags) != 2 || !isHex(flags) {
		return IDs{}, false
	}

	return IDs{TraceID: strings.ToLower(traceID), SpanID: strings.ToLower(spanID)}, true
}

// WithContext stores tracing identifiers in ctx.
func WithContext(ctx context.Context, ids IDs) context.Context {
	return context.WithValue(ctx, contextKey{}, ids)
}

// FromContext returns the tracing identifiers stored in ctx.
func FromContext(ctx context.Context) (IDs, bool) {
	ids, ok := ctx.Value(contextKey{}).(IDs)
	if !ok || ids.TraceID == "" || ids.SpanID == "" {
		return IDs{}, false
	}
	return ids, true
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(buf)
}

func isHex(value string) bool {
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func allZero(value string) bool {
	for _, r := range value {
		if r != '0' {
			return false
		}
	}
	return true
}
