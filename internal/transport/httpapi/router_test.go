package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
)

var testNow = time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)

type sequentialIDs struct {
	next int
}

func (s *sequentialIDs) NewID() string {
	s.next++
	return fmt.Sprintf("prc_%d", s.next)
}

func (s *sequentialIDs) NewNumber(now time.Time) string {
	return fmt.Sprintf("PR-%s-%04d", now.Format("200601"), s.next)
}

func buildRouter(t *testing.T, repo *memory.ProcurementRepository, checks map[string]handler.Check) http.Handler {
	t.Helper()

	service := app.NewService(repo, &sequentialIDs{}, func() time.Time { return testNow })
	health := handler.NewHealth("test", checks)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := config.Config{
		Env:      config.EnvDevelopment,
		LogLevel: slog.LevelInfo,
		HTTP: config.HTTP{
			Addr:           ":0",
			RequestTimeout: 5 * time.Second,
		},
	}

	return httpapi.NewRouter(httpapi.Dependencies{
		Config:      cfg,
		Logger:      logger,
		Procurement: service,
		Health:      health,
	})
}

func newTestRouter(t *testing.T) (http.Handler, *memory.ProcurementRepository) {
	t.Helper()

	repo := memory.NewProcurementRepository()
	return buildRouter(t, repo, map[string]handler.Check{"repository": repo.Ping}), repo
}

func doRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return doRequestWithHeaders(h, method, path, body, "application/json", map[string]string{
		"X-Request-ID": "test-request",
	})
}

func doRequestWithHeaders(
	h http.Handler,
	method, path, body, contentType string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type envelope struct {
	Data json.RawMessage `json:"data"`
	Meta json.RawMessage `json:"meta"`
}

type procurementBody struct {
	ID              string    `json:"id"`
	Number          string    `json:"number"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Department      string    `json:"department"`
	Requester       string    `json:"requester"`
	RequesterID     string    `json:"requester_id"`
	Amount          int64     `json:"amount"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	RejectionReason string    `json:"rejection_reason"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Version         int       `json:"version"`
}

type pageMeta struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type problemBody struct {
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Status    int               `json:"status"`
	Detail    string            `json:"detail"`
	Instance  string            `json:"instance"`
	Code      string            `json:"code"`
	RequestID string            `json:"request_id"`
	Errors    map[string]string `json:"errors"`
}

type healthBody struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type readyBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	return value
}

func decodeRaw[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("field is not valid JSON: %v (raw=%q)", err, raw)
	}
	return value
}

func createProcurement(t *testing.T, h http.Handler, title, department string) procurementBody {
	t.Helper()

	body := fmt.Sprintf(
		`{"title":%q,"description":"test description","department":%q,"requester":"Amir","amount":1000000,"currency":"IDR"}`,
		title,
		department,
	)
	rec := doRequest(h, http.MethodPost, "/api/v1/procurements", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	return decodeRaw[procurementBody](t, decodeBody[envelope](t, rec).Data)
}

func requireProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) problemBody {
	t.Helper()

	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, status, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	problem := decodeBody[problemBody](t, rec)
	if problem.Status != status {
		t.Errorf("problem.status = %d, want %d", problem.Status, status)
	}
	if problem.Code != code {
		t.Errorf("problem.code = %q, want %q", problem.Code, code)
	}
	if problem.Type != "about:blank" {
		t.Errorf("problem.type = %q, want about:blank", problem.Type)
	}
	return problem
}

func TestCreateGetAndList(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequest(h, http.MethodPost, "/api/v1/procurements",
		`{"title":"Laptop procurement for engineering","description":"20 units","department":"Engineering","requester":"Amir","amount":250000000,"currency":"idr"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/api/v1/procurements/prc_1" {
		t.Errorf("Location = %q, want /api/v1/procurements/prc_1", got)
	}

	created := decodeRaw[procurementBody](t, decodeBody[envelope](t, rec).Data)
	if created.ID != "prc_1" {
		t.Errorf("id = %q, want prc_1", created.ID)
	}
	if created.Number != "PR-202610-0001" {
		t.Errorf("number = %q, want PR-202610-0001", created.Number)
	}
	if created.Status != "draft" {
		t.Errorf("status = %q, want draft", created.Status)
	}
	if created.Currency != "IDR" {
		t.Errorf("currency = %q, want IDR", created.Currency)
	}
	if created.Version != 1 {
		t.Errorf("version = %d, want 1", created.Version)
	}
	if created.CreatedAt.IsZero() {
		t.Error("created_at is zero")
	}

	rec = doRequest(h, http.MethodGet, "/api/v1/procurements/prc_1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", rec.Code)
	}
	got := decodeRaw[procurementBody](t, decodeBody[envelope](t, rec).Data)
	if got.ID != created.ID || got.Title != created.Title {
		t.Errorf("get = %+v, want created record", got)
	}

	rec = doRequest(h, http.MethodGet, "/api/v1/procurements", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	env := decodeBody[envelope](t, rec)
	items := decodeRaw[[]procurementBody](t, env.Data)
	meta := decodeRaw[pageMeta](t, env.Meta)
	if len(items) != 1 || items[0].ID != created.ID {
		t.Errorf("items = %+v, want one created item", items)
	}
	if meta.Total != 1 || meta.Page != 1 || meta.PageSize != 20 || meta.TotalPages != 1 {
		t.Errorf("meta = %+v, want page=1 size=20 total=1 pages=1", meta)
	}
}

func TestCreateValidation(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequest(h, http.MethodPost, "/api/v1/procurements",
		`{"title":"","department":"Engineering","requester":"Amir","amount":1000000,"currency":"IDR"}`)

	problem := requireProblem(t, rec, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	if problem.Errors["title"] == "" {
		t.Errorf("errors.title = %q, want a validation message", problem.Errors["title"])
	}
	if problem.RequestID != "test-request" {
		t.Errorf("request_id = %q, want test-request", problem.RequestID)
	}
}

func TestCreateInvalidJSON(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequest(h, http.MethodPost, "/api/v1/procurements", `{"title":`)

	requireProblem(t, rec, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestCreateWrongContentType(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequestWithHeaders(h, http.MethodPost, "/api/v1/procurements",
		`{"title":"Laptop procurement"}`, "text/plain", nil)

	requireProblem(t, rec, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
}

func TestCreateBodyTooLarge(t *testing.T) {
	h, _ := newTestRouter(t)

	body := `{"title":"` + strings.Repeat("a", 1<<20) + `","department":"Engineering","requester":"Amir","amount":1,"currency":"IDR"}`
	rec := doRequest(h, http.MethodPost, "/api/v1/procurements", body)

	requireProblem(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
}

func TestGetNotFound(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequest(h, http.MethodGet, "/api/v1/procurements/missing", "")

	requireProblem(t, rec, http.StatusNotFound, "NOT_FOUND")
}

func TestListFiltersAndPagination(t *testing.T) {
	h, _ := newTestRouter(t)

	laptop := createProcurement(t, h, "Laptop engineering", "Engineering")
	createProcurement(t, h, "Office chairs", "Facilities")
	laptopSupport := createProcurement(t, h, "Laptop support", "Support")

	rec := doRequest(h, http.MethodPost, "/api/v1/procurements/"+laptop.ID+"/submit", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("submit status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	t.Run("status filter", func(t *testing.T) {
		rec := doRequest(h, http.MethodGet, "/api/v1/procurements?status=submitted", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		env := decodeBody[envelope](t, rec)
		items := decodeRaw[[]procurementBody](t, env.Data)
		meta := decodeRaw[pageMeta](t, env.Meta)
		if meta.Total != 1 || len(items) != 1 || items[0].ID != laptop.ID {
			t.Errorf("filtered items = %+v meta=%+v, want only %s", items, meta, laptop.ID)
		}
	})

	t.Run("search filter", func(t *testing.T) {
		rec := doRequest(h, http.MethodGet, "/api/v1/procurements?q=laptop", "")
		env := decodeBody[envelope](t, rec)
		meta := decodeRaw[pageMeta](t, env.Meta)
		if meta.Total != 2 {
			t.Errorf("search total = %d, want 2", meta.Total)
		}
		if laptopSupport.ID == "" {
			t.Error("laptopSupport ID is empty")
		}
	})

	t.Run("pagination", func(t *testing.T) {
		rec := doRequest(h, http.MethodGet, "/api/v1/procurements?page=1&page_size=2", "")
		env := decodeBody[envelope](t, rec)
		items := decodeRaw[[]procurementBody](t, env.Data)
		meta := decodeRaw[pageMeta](t, env.Meta)
		if len(items) != 2 || meta.Total != 3 || meta.TotalPages != 2 || meta.PageSize != 2 {
			t.Errorf("items=%d meta=%+v, want 2 items and 2 pages", len(items), meta)
		}
	})

	t.Run("invalid query values", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/procurements?page=abc",
			"/api/v1/procurements?page_size=1000",
			"/api/v1/procurements?status=unknown",
		} {
			rec := doRequest(h, http.MethodGet, path, "")
			requireProblem(t, rec, http.StatusBadRequest, "INVALID_QUERY")
		}
	})
}

func TestWorkflow(t *testing.T) {
	h, _ := newTestRouter(t)
	created := createProcurement(t, h, "Laptop procurement", "Engineering")

	rec := doRequest(h, http.MethodPost, "/api/v1/procurements/"+created.ID+"/submit", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("submit status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	submitted := decodeRaw[procurementBody](t, decodeBody[envelope](t, rec).Data)
	if submitted.Status != "submitted" || submitted.Version != 2 {
		t.Errorf("submitted = status %q version %d, want submitted/2", submitted.Status, submitted.Version)
	}

	rec = doRequest(h, http.MethodPost, "/api/v1/procurements/"+created.ID+"/approve", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	approved := decodeRaw[procurementBody](t, decodeBody[envelope](t, rec).Data)
	if approved.Status != "approved" || approved.Version != 3 {
		t.Errorf("approved = status %q version %d, want approved/3", approved.Status, approved.Version)
	}

	rec = doRequest(h, http.MethodPost, "/api/v1/procurements/"+created.ID+"/approve", "")
	requireProblem(t, rec, http.StatusConflict, "INVALID_TRANSITION")

	rec = doRequest(h, http.MethodPost, "/api/v1/procurements/"+created.ID+"/cancel", "")
	requireProblem(t, rec, http.StatusConflict, "INVALID_TRANSITION")

	second := createProcurement(t, h, "Office chairs", "Facilities")
	rec = doRequest(h, http.MethodPost, "/api/v1/procurements/"+second.ID+"/submit", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("second submit status = %d, want 200", rec.Code)
	}

	rec = doRequest(h, http.MethodPost, "/api/v1/procurements/"+second.ID+"/reject", `{"reason":""}`)
	problem := requireProblem(t, rec, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	if problem.Errors["reason"] == "" {
		t.Errorf("errors.reason = %q, want a validation message", problem.Errors["reason"])
	}

	rec = doRequest(h, http.MethodPost, "/api/v1/procurements/"+second.ID+"/reject", `{"reason":"budget exceeded"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	rejected := decodeRaw[procurementBody](t, decodeBody[envelope](t, rec).Data)
	if rejected.Status != "rejected" || rejected.RejectionReason != "budget exceeded" {
		t.Errorf("rejected = status %q reason %q, want rejected/budget exceeded", rejected.Status, rejected.RejectionReason)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequest(h, http.MethodDelete, "/api/v1/procurements", "")

	requireProblem(t, rec, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
	allow := rec.Header().Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Errorf("Allow = %q, want GET and POST", allow)
	}
}

func TestUnknownRoute(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequest(h, http.MethodGet, "/api/v1/unknown", "")

	requireProblem(t, rec, http.StatusNotFound, "NOT_FOUND")
}

func TestHealth(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequest(h, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
	live := decodeRaw[healthBody](t, decodeBody[envelope](t, rec).Data)
	if live.Status != "ok" || live.Version != "test" {
		t.Errorf("healthz = %+v, want ok/test", live)
	}

	rec = doRequest(h, http.MethodGet, "/readyz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200", rec.Code)
	}
	ready := decodeRaw[readyBody](t, decodeBody[envelope](t, rec).Data)
	if ready.Status != "ready" || ready.Checks["repository"] != "ok" {
		t.Errorf("readyz = %+v, want ready with repository ok", ready)
	}
}

func TestReadyFailure(t *testing.T) {
	repo := memory.NewProcurementRepository()
	h := buildRouter(t, repo, map[string]handler.Check{
		"database": func(ctx context.Context) error {
			return errors.New("database is down")
		},
	})

	rec := doRequest(h, http.MethodGet, "/readyz", "")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", rec.Code)
	}
	ready := decodeRaw[readyBody](t, decodeBody[envelope](t, rec).Data)
	if ready.Status != "not_ready" || ready.Checks["database"] != "error" {
		t.Errorf("readyz = %+v, want not_ready with database error", ready)
	}
}

func TestRequestIDIsGeneratedAndEchoed(t *testing.T) {
	h, _ := newTestRouter(t)

	rec := doRequestWithHeaders(h, http.MethodGet, "/healthz", "", "", nil)

	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("X-Request-ID response header is empty")
	}
	if len(requestID) != 32 {
		t.Errorf("X-Request-ID = %q, want 32 hex chars", requestID)
	}
}
