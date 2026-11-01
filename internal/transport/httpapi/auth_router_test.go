package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
	"github.com/amirfaisalz/procure-flow/internal/platform/metrics"
	"github.com/amirfaisalz/procure-flow/internal/platform/ratelimit"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
)

const (
	adminToken     = "test-admin-token-0123456789"
	requesterToken = "test-requester-token-0123456789"
	otherToken     = "test-other-token-0123456789"
	approverToken  = "test-approver-token-0123456789"
	viewerToken    = "test-viewer-token-0123456789"
)

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func buildAuthenticatedRouter(
	t *testing.T,
	repo *memory.ProcurementRepository,
	auditRepo *memory.AuditRepository,
	opts ...func(*httpapi.Dependencies),
) http.Handler {
	t.Helper()

	service := app.NewService(
		repo,
		&sequentialIDs{},
		func() time.Time { return testNow },
		app.WithAudit(auditRepo, nil),
	)
	health := handler.NewHealthWithOptional("test", map[string]handler.Check{"repository": repo.Ping}, nil)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	authenticator := auth.NewAPIKeyAuthenticator([]auth.APIKey{
		{Token: adminToken, Principal: auth.Principal{Subject: "user_admin", Name: "Admin", Roles: []auth.Role{auth.RoleAdmin}}},
		{Token: requesterToken, Principal: auth.Principal{Subject: "user_amir", Name: "Amir", Roles: []auth.Role{auth.RoleRequester}, Department: "Engineering"}},
		{Token: otherToken, Principal: auth.Principal{Subject: "user_budi", Name: "Budi", Roles: []auth.Role{auth.RoleRequester}, Department: "Engineering"}},
		{Token: approverToken, Principal: auth.Principal{Subject: "user_fin", Name: "Fin", Roles: []auth.Role{auth.RoleApprover}, Department: "Engineering"}},
		{Token: viewerToken, Principal: auth.Principal{Subject: "user_viewer", Name: "Viewer", Roles: []auth.Role{auth.RoleViewer}}},
	})

	deps := httpapi.Dependencies{
		Config: config.Config{
			Env:         config.EnvDevelopment,
			Auth:        config.Auth{Enabled: true},
			HTTP:        config.HTTP{Addr: ":0", RequestTimeout: 5 * time.Second},
			Idempotency: config.Idempotency{Enabled: true, TTL: time.Hour},
			Metrics:     config.Metrics{Enabled: true},
		},
		Logger:        logger,
		Procurement:   service,
		Health:        health,
		Authenticator: authenticator,
		Idempotency:   memory.NewIdempotencyRepository(),
		Metrics:       metrics.NewRegistry(),
	}
	for _, opt := range opts {
		opt(&deps)
	}
	return httpapi.NewRouter(deps)
}

func TestRouterRejectsAnonymousAPICalls(t *testing.T) {
	repo := memory.NewProcurementRepository()
	router := buildAuthenticatedRouter(t, repo, memory.NewAuditRepository())

	rec := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	problem := decodeBody[problemBody](t, rec)
	if problem.Code != "UNAUTHENTICATED" {
		t.Errorf("code = %q, want UNAUTHENTICATED", problem.Code)
	}

	// Health and documentation stay public.
	if rec := doRequestWithHeaders(router, http.MethodGet, "/healthz", "", "", nil); rec.Code != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", rec.Code)
	}
}

func TestRouterEnforcesRBACAndRecordsAudit(t *testing.T) {
	repo := memory.NewProcurementRepository()
	auditRepo := memory.NewAuditRepository()
	router := buildAuthenticatedRouter(t, repo, auditRepo)

	const body = `{"title":"Laptop procurement for engineering","description":"20 units","department":"Engineering","requester":"","amount":250000000,"currency":"IDR"}`

	// A viewer cannot create.
	rec := doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements", body, "application/json", bearer(viewerToken))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}

	// A requester creates and the requester is stamped from the token.
	rec = doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements", body, "application/json", bearer(requesterToken))
	if rec.Code != http.StatusCreated {
		t.Fatalf("requester create status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[envelope](t, rec)
	var procurement procurementBody
	if err := json.Unmarshal(created.Data, &procurement); err != nil {
		t.Fatalf("decode procurement: %v", err)
	}
	if procurement.RequesterID != "user_amir" || procurement.Requester != "Amir" {
		t.Errorf("requester = %q/%q, want user_amir/Amir", procurement.RequesterID, procurement.Requester)
	}

	// Another requester cannot submit it.
	rec = doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements/"+procurement.ID+"/submit", "", "", bearer(otherToken))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other submit status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}

	// The owner submits it.
	rec = doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements/"+procurement.ID+"/submit", "", "", bearer(requesterToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("submit status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// A matching-department approver can approve it.
	rec = doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements/"+procurement.ID+"/approve", "", "", bearer(approverToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// The audit trail is readable and newest first.
	rec = doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements/"+procurement.ID+"/audit", "", "", bearer(viewerToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("audit status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var auditEnvelope struct {
		Data []struct {
			Action    string `json:"action"`
			ActorID   string `json:"actor_id"`
			ActorRole string `json:"actor_role"`
			ToStatus  string `json:"to_status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &auditEnvelope); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(auditEnvelope.Data) != 3 {
		t.Fatalf("audit events = %d, want 3", len(auditEnvelope.Data))
	}
	wantActions := []string{app.ActionApproved, app.ActionSubmitted, app.ActionCreated}
	for i, want := range wantActions {
		if auditEnvelope.Data[i].Action != want {
			t.Errorf("events[%d].Action = %q, want %q", i, auditEnvelope.Data[i].Action, want)
		}
	}
	if auditEnvelope.Data[0].ActorID != "user_fin" || auditEnvelope.Data[0].ActorRole != app.RoleApprover {
		t.Errorf("approval actor = %+v, want the approver", auditEnvelope.Data[0])
	}
}

func TestRouterIdempotentCreate(t *testing.T) {
	repo := memory.NewProcurementRepository()
	router := buildAuthenticatedRouter(t, repo, memory.NewAuditRepository())

	const body = `{"title":"Laptop procurement for engineering","description":"20 units","department":"Engineering","amount":250000000,"currency":"IDR"}`
	headers := bearer(requesterToken)
	headers["Idempotency-Key"] = "create-1"

	first := doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements", body, "application/json", headers)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201; body = %s", first.Code, first.Body.String())
	}
	second := doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements", body, "application/json", headers)
	if second.Code != http.StatusCreated {
		t.Fatalf("second status = %d, want 201; body = %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("second response was not marked as replayed")
	}

	firstBody := decodeBody[envelope](t, first)
	secondBody := decodeBody[envelope](t, second)
	var firstItem, secondItem procurementBody
	if err := json.Unmarshal(firstBody.Data, &firstItem); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.Unmarshal(secondBody.Data, &secondItem); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if firstItem.ID != secondItem.ID {
		t.Errorf("ids = %q and %q, want the same procurement", firstItem.ID, secondItem.ID)
	}

	list := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", bearer(adminToken))
	var page struct {
		Data []procurementBody `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(page.Data) != 1 {
		t.Errorf("procurements = %d, want 1 (the retry must not create a duplicate)", len(page.Data))
	}

	reused := doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements",
		`{"title":"Server procurement for engineering","department":"Engineering","amount":1000,"currency":"IDR"}`,
		"application/json", headers)
	if reused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reused status = %d, want 422; body = %s", reused.Code, reused.Body.String())
	}
}

func TestRouterRateLimit(t *testing.T) {
	repo := memory.NewProcurementRepository()
	router := buildAuthenticatedRouter(t, repo, memory.NewAuditRepository(), func(deps *httpapi.Dependencies) {
		deps.Config.RateLimit = config.RateLimit{Enabled: true, RequestsPerSecond: 1, Burst: 1}
		deps.RateLimiter = ratelimit.New(1, 1)
	})

	if rec := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", bearer(adminToken)); rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	rec := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", bearer(adminToken))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429; body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header is missing")
	}
}

func TestRouterRateLimitThrottlesUnauthenticatedRequests(t *testing.T) {
	repo := memory.NewProcurementRepository()
	router := buildAuthenticatedRouter(t, repo, memory.NewAuditRepository(), func(deps *httpapi.Dependencies) {
		deps.Config.RateLimit = config.RateLimit{Enabled: true, RequestsPerSecond: 1, Burst: 1}
		deps.RateLimiter = ratelimit.New(1, 1)
	})

	if rec := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("first status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	// The failed attempt consumed the per-IP bucket, so credential guessing is
	// throttled instead of returning unlimited 401s.
	rec := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429; body = %s", rec.Code, rec.Body.String())
	}
}

func TestRouterMetricsAndTracing(t *testing.T) {
	repo := memory.NewProcurementRepository()
	router := buildAuthenticatedRouter(t, repo, memory.NewAuditRepository())

	health := doRequestWithHeaders(router, http.MethodGet, "/healthz", "", "", nil)
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}
	if health.Header().Get("traceparent") == "" {
		t.Error("traceparent response header is missing")
	}
	if health.Header().Get("X-Trace-ID") == "" {
		t.Error("X-Trace-ID response header is missing")
	}

	// The API sub-router replaces the request value during authentication, so
	// this also asserts that nested route patterns reach the metrics recorder.
	list := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", bearer(viewerToken))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body = %s", list.Code, list.Body.String())
	}

	rec := doRequestWithHeaders(router, http.MethodGet, "/metrics", "", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `http_requests_total{method="GET",route="GET /healthz",status="200"} 1`) {
		t.Errorf("metrics output is missing the health counter\n%s", body)
	}
	if !strings.Contains(body, `http_request_duration_seconds_count{method="GET",route="GET /healthz",status="200"} 1`) {
		t.Errorf("metrics output is missing the health histogram\n%s", body)
	}
	if !strings.Contains(body, `http_requests_total{method="GET",route="GET /api/v1/procurements",status="200"} 1`) {
		t.Errorf("metrics output is missing the nested API route counter\n%s", body)
	}
}

func TestRouterAuditPagination(t *testing.T) {
	repo := memory.NewProcurementRepository()
	router := buildAuthenticatedRouter(t, repo, memory.NewAuditRepository())

	const body = `{"title":"Laptop procurement for engineering","department":"Engineering","amount":250000000,"currency":"IDR"}`
	created := doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements", body, "application/json", bearer(requesterToken))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body = %s", created.Code, created.Body.String())
	}
	var envelope struct {
		Data procurementBody `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements/"+envelope.Data.ID+"/submit", "", "", bearer(requesterToken))
	doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements/"+envelope.Data.ID+"/approve", "", "", bearer(approverToken))

	rec := doRequestWithHeaders(router, http.MethodGet,
		"/api/v1/procurements/"+envelope.Data.ID+"/audit?page=1&page_size=2", "", "", bearer(viewerToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("audit status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Data []struct {
			Action string `json:"action"`
		} `json:"data"`
		Meta pageMeta `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(result.Data) != 2 {
		t.Errorf("events = %d, want 2", len(result.Data))
	}
	if result.Meta.Total != 3 || result.Meta.Page != 1 || result.Meta.PageSize != 2 || result.Meta.TotalPages != 2 {
		t.Errorf("meta = %+v, want total=3 page=1 size=2 pages=2", result.Meta)
	}

	invalid := doRequestWithHeaders(router, http.MethodGet,
		"/api/v1/procurements/"+envelope.Data.ID+"/audit?page=0", "", "", bearer(viewerToken))
	if invalid.Code != http.StatusBadRequest {
		t.Errorf("invalid page status = %d, want 400", invalid.Code)
	}
}
