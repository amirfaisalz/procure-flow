package middleware_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/middleware"
)

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func serve(h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestChainOrder(t *testing.T) {
	var order []string

	marker := func(name string) middleware.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
				order = append(order, name+"-after")
			})
		}
	}

	h := middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	}), marker("a"), marker("b"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"a", "b", "handler", "b-after", "a-after"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestRequestIDHonorsValidIncomingHeader(t *testing.T) {
	var got string
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = requestid.From(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := serve(h, http.MethodGet, "/", map[string]string{"X-Request-ID": "req-123"})

	if got != "req-123" {
		t.Errorf("context request ID = %q, want req-123", got)
	}
	if rec.Header().Get("X-Request-ID") != "req-123" {
		t.Errorf("response header = %q, want req-123", rec.Header().Get("X-Request-ID"))
	}
}

func TestRequestIDGeneratesWhenMissing(t *testing.T) {
	var got string
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = requestid.From(r.Context())
	}))

	rec := serve(h, http.MethodGet, "/", nil)

	if got == "" {
		t.Fatal("context request ID is empty")
	}
	if len(got) != 32 {
		t.Errorf("generated request ID = %q, want 32 hex chars", got)
	}
	if rec.Header().Get("X-Request-ID") != got {
		t.Errorf("response header = %q, want %q", rec.Header().Get("X-Request-ID"), got)
	}
}

func TestRequestIDReplacesInvalidHeader(t *testing.T) {
	var got string
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = requestid.From(r.Context())
	}))

	rec := serve(h, http.MethodGet, "/", map[string]string{"X-Request-ID": "bad id!"})

	if got == "bad id!" {
		t.Fatal("invalid request ID was accepted")
	}
	if len(got) != 32 {
		t.Errorf("generated request ID = %q, want 32 hex chars", got)
	}
	if rec.Header().Get("X-Request-ID") != got {
		t.Errorf("response header = %q, want %q", rec.Header().Get("X-Request-ID"), got)
	}
}

func TestRecoverReturnsProblemJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf)

	h := middleware.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}),
		middleware.RequestID(),
		middleware.Recover(logger),
	)

	rec := serve(h, http.MethodGet, "/api/v1/test", map[string]string{"X-Request-ID": "req-panic"})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}

	var body struct {
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", body.Code)
	}
	if body.RequestID != "req-panic" {
		t.Errorf("request_id = %q, want req-panic", body.RequestID)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "panic recovered") || !strings.Contains(logOutput, "boom") {
		t.Errorf("log output = %q, want panic details", logOutput)
	}
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	h := middleware.Recover(testLogger(&bytes.Buffer{}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	panicked := false
	func() {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			panicked = true
			if recovered != http.ErrAbortHandler {
				t.Errorf("recovered = %v, want http.ErrAbortHandler", recovered)
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	if !panicked {
		t.Fatal("ErrAbortHandler was swallowed")
	}
}

func TestAccessLog(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf)

	h := middleware.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("hello"))
		}),
		middleware.RequestID(),
		middleware.AccessLog(logger),
	)

	serve(h, http.MethodPost, "/api/v1/procurements?page=2", map[string]string{"X-Request-ID": "req-log"})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log is not JSON: %v (output=%q)", err, buf.String())
	}
	if entry["msg"] != "http request" {
		t.Errorf("msg = %v, want http request", entry["msg"])
	}
	if entry["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", entry["level"])
	}
	if entry["method"] != "POST" {
		t.Errorf("method = %v, want POST", entry["method"])
	}
	if entry["path"] != "/api/v1/procurements" {
		t.Errorf("path = %v, want /api/v1/procurements", entry["path"])
	}
	if entry["query"] != "page=2" {
		t.Errorf("query = %v, want page=2", entry["query"])
	}
	if entry["status"] != float64(http.StatusCreated) {
		t.Errorf("status = %v, want 201", entry["status"])
	}
	if entry["bytes"] != float64(5) {
		t.Errorf("bytes = %v, want 5", entry["bytes"])
	}
	if entry["request_id"] != "req-log" {
		t.Errorf("request_id = %v, want req-log", entry["request_id"])
	}
}

func TestAccessLogDefaultsToOK(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf)

	h := middleware.AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serve(h, http.MethodGet, "/", nil)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log is not JSON: %v", err)
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", entry["status"])
	}
}

func TestAccessLogUsesWarnForClientErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf)

	h := middleware.AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	serve(h, http.MethodGet, "/", nil)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log is not JSON: %v", err)
	}
	if entry["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", entry["level"])
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := middleware.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	}
	for key, value := range want {
		if got := rec.Header().Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy is empty")
	}
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("Strict-Transport-Security is empty for a TLS request")
	}
}

func TestSecurityHeadersSkipsHSTSWithoutTLS(t *testing.T) {
	h := middleware.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := serve(h, http.MethodGet, "/", nil)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want empty", got)
	}
}

func TestCORS(t *testing.T) {
	allowed := []string{"https://app.example.com"}
	h := middleware.CORS(allowed)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed preflight", func(t *testing.T) {
		rec := serve(h, http.MethodOptions, "/api/v1/procurements", map[string]string{
			"Origin":                        "https://app.example.com",
			"Access-Control-Request-Method": "POST",
		})

		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
			t.Errorf("Allow-Origin = %q, want allowed origin", got)
		}
		if rec.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("Allow-Methods is empty")
		}
		if allowHeaders := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(allowHeaders, "Idempotency-Key") {
			t.Errorf("Allow-Headers = %q, want Idempotency-Key", allowHeaders)
		}
		if expose := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(expose, "Idempotent-Replayed") {
			t.Errorf("Expose-Headers = %q, want Idempotent-Replayed", expose)
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Errorf("Vary = %q, want Origin", rec.Header().Get("Vary"))
		}
	})

	t.Run("disallowed preflight", func(t *testing.T) {
		rec := serve(h, http.MethodOptions, "/api/v1/procurements", map[string]string{
			"Origin": "https://evil.example.com",
		})

		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q, want empty", got)
		}
	})

	t.Run("allowed simple request", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/", map[string]string{"Origin": "https://app.example.com"})

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
			t.Errorf("Allow-Origin = %q, want allowed origin", got)
		}
	})

	t.Run("no origin", func(t *testing.T) {
		rec := serve(h, http.MethodGet, "/", nil)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q, want empty", got)
		}
	})
}

func TestTimeoutCancelsContext(t *testing.T) {
	var ctxErr error
	h := middleware.Timeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		ctxErr = r.Context().Err()
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !errors.Is(ctxErr, context.DeadlineExceeded) {
		t.Errorf("context error = %v, want DeadlineExceeded", ctxErr)
	}
}
