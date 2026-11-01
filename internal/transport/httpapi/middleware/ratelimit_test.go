package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/platform/ratelimit"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/middleware"
)

func serveRateLimited(t *testing.T, h http.Handler, actor string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
	req.RemoteAddr = "203.0.113.7:12345"
	if actor != "" {
		req = req.WithContext(app.WithActor(req.Context(), app.Actor{Subject: actor}))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRateLimitAllowsWithinBurst(t *testing.T) {
	limiter := ratelimit.New(1, 2)
	h := middleware.RateLimit(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		rec := serveRateLimited(t, h, "user_amir")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, rec.Code)
		}
		if got := rec.Header().Get("X-RateLimit-Limit"); got != "2" {
			t.Errorf("X-RateLimit-Limit = %q, want 2", got)
		}
	}
}

func TestRateLimitRejectsExcessRequests(t *testing.T) {
	limiter := ratelimit.New(1, 1)
	h := middleware.RateLimit(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	if rec := serveRateLimited(t, h, "user_amir"); rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", rec.Code)
	}

	rec := serveRateLimited(t, h, "user_amir")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header is missing")
	}

	var problem struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Status != http.StatusTooManyRequests || problem.Code != "RATE_LIMITED" {
		t.Errorf("problem = %+v, want 429 RATE_LIMITED", problem)
	}
}

func TestRateLimitIsScopedPerActor(t *testing.T) {
	limiter := ratelimit.New(1, 1)
	h := middleware.RateLimit(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	if rec := serveRateLimited(t, h, "user_amir"); rec.Code != http.StatusOK {
		t.Fatalf("amir status = %d", rec.Code)
	}
	if rec := serveRateLimited(t, h, "user_budi"); rec.Code != http.StatusOK {
		t.Fatalf("budi status = %d, want 200", rec.Code)
	}
}

func TestRateLimitFallsBackToRemoteIP(t *testing.T) {
	limiter := ratelimit.New(1, 1)
	h := middleware.RateLimit(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	if rec := serveRateLimited(t, h, ""); rec.Code != http.StatusOK {
		t.Fatalf("first anonymous status = %d, want 200", rec.Code)
	}
	if rec := serveRateLimited(t, h, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second anonymous status = %d, want 429", rec.Code)
	}
}

func TestRateLimitDisabledLimiterAllowsAll(t *testing.T) {
	h := middleware.RateLimit(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		if rec := serveRateLimited(t, h, "user_amir"); rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, rec.Code)
		}
	}
}
