package handler

import (
	"net/http"

	"github.com/amirfaisalz/procure-flow/internal/platform/metrics"
)

// Metrics serves the Prometheus text exposition endpoint.
type Metrics struct {
	registry *metrics.Registry
}

// NewMetrics creates the metrics handler.
func NewMetrics(registry *metrics.Registry) *Metrics {
	return &Metrics{registry: registry}
}

// ServeHTTP handles GET /metrics.
func (h *Metrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if h.registry != nil {
		_ = h.registry.WritePrometheus(w)
	}
}
