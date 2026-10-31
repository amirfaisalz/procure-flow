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

// Health serves liveness and readiness probes.
type Health struct {
	version string
	checks  map[string]Check
}

// NewHealth creates a health handler. Checks are evaluated on every readiness
// request so transient dependency failures are reflected immediately.
func NewHealth(version string, checks map[string]Check) *Health {
	return &Health{version: version, checks: checks}
}

// Live reports that the process is running.
func (h *Health) Live(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, liveResponse{Status: "ok", Version: h.version})
}

// Ready reports whether all dependencies are usable. It returns 503 when any
// check fails so load balancers stop routing traffic to this instance.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	results := make(map[string]string, len(h.checks))
	ready := true

	for name, check := range h.checks {
		if err := check(r.Context()); err != nil {
			results[name] = "error"
			ready = false
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

	response.JSON(w, http.StatusOK, readyResponse{
		Status: "ready",
		Checks: results,
	})
}
