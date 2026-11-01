package metrics_test

import (
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/platform/metrics"
)

func render(t *testing.T, registry *metrics.Registry) string {
	t.Helper()
	var b strings.Builder
	if err := registry.WritePrometheus(&b); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}
	return b.String()
}

func TestObserveRendersCounterAndHistogram(t *testing.T) {
	registry := metrics.NewRegistry()
	registry.Observe("GET", "/api/v1/procurements", 200, 12*time.Millisecond)
	registry.Observe("GET", "/api/v1/procurements", 200, 3*time.Millisecond)
	registry.Observe("POST", "/api/v1/procurements", 201, 150*time.Millisecond)

	out := render(t, registry)

	want := []string{
		`http_requests_total{method="GET",route="/api/v1/procurements",status="200"} 2`,
		`http_requests_total{method="POST",route="/api/v1/procurements",status="201"} 1`,
		`http_request_duration_seconds_bucket{method="GET",route="/api/v1/procurements",status="200",le="0.01"} 1`,
		`http_request_duration_seconds_bucket{method="GET",route="/api/v1/procurements",status="200",le="0.025"} 2`,
		`http_request_duration_seconds_bucket{method="GET",route="/api/v1/procurements",status="200",le="+Inf"} 2`,
		`http_request_duration_seconds_count{method="GET",route="/api/v1/procurements",status="200"} 2`,
	}
	for _, line := range want {
		if !strings.Contains(out, line) {
			t.Errorf("output is missing %q\n%s", line, out)
		}
	}
}

func TestObserveDefaultsEmptyRoute(t *testing.T) {
	registry := metrics.NewRegistry()
	registry.Observe("GET", "", 404, time.Millisecond)

	if out := render(t, registry); !strings.Contains(out, `route="unmatched"`) {
		t.Errorf("output is missing the unmatched route label\n%s", out)
	}
}

func TestWritePrometheusEscapesLabels(t *testing.T) {
	registry := metrics.NewRegistry()
	registry.Observe("GET", `/odd"path\`, 500, time.Millisecond)

	out := render(t, registry)
	if !strings.Contains(out, `route="/odd\"path\\"`) {
		t.Errorf("label was not escaped\n%s", out)
	}
}

func TestNilRegistryIsSafe(t *testing.T) {
	var registry *metrics.Registry
	registry.Observe("GET", "/", 200, time.Second)
	if err := registry.WritePrometheus(&strings.Builder{}); err != nil {
		t.Errorf("WritePrometheus() error = %v", err)
	}
}
