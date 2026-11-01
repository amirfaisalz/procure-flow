package handler

import (
	"context"
	"net/http"

	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

// Check is a readiness probe for one dependency.
type Check func(context.Context) error

type liveResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Health serves liveness and readiness probes. Required checks gate readiness;
// optional checks (Redis, RabbitMQ) are reported but only degrade the status, so
// a missing optimization cannot pull every replica out of the load balancer.
type Health struct {
	version  string
	checks   map[string]Check
	optional map[string]Check
}

// NewHealthWithOptional creates a health handler with required and optional
// checks. Only required failures return 503.
func NewHealthWithOptional(version string, required, optional map[string]Check) *Health {
	return &Health{version: version, checks: required, optional: optional}
}

// Live reports that the process is running.
func (h *Health) Live(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, liveResponse{Status: "ok", Version: h.version})
}

// Ready reports whether all required dependencies are usable. It returns 503
// when a required check fails; a failing optional check reports "degraded" with
// 200 so the instance keeps receiving traffic.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	results := make(map[string]string, len(h.checks)+len(h.optional))
	ready := true

	for name, check := range h.checks {
		if err := check(r.Context()); err != nil {
			results[name] = "error"
			ready = false
			continue
		}
		results[name] = "ok"
	}

	degraded := false
	for name, check := range h.optional {
		if err := check(r.Context()); err != nil {
			results[name] = "unavailable"
			degraded = true
			continue
		}
		results[name] = "ok"
	}

	if !ready {
		response.JSON(w, http.StatusServiceUnavailable, readyResponse{
			Status: "not_ready",
			Checks: results,
		})
		return
	}

	status := "ready"
	if degraded {
		status = "degraded"
	}
	response.JSON(w, http.StatusOK, readyResponse{
		Status: status,
		Checks: results,
	})
}
