package httpapi_test

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
)

func buildDocsRouter(t *testing.T, enabled bool) http.Handler {
	t.Helper()

	repo := testsupport.NewProcurementRepository()
	service := app.NewService(repo, &sequentialIDs{}, func() time.Time { return testNow })
	health := handler.NewHealthWithOptional("test", map[string]handler.Check{"repository": repo.Ping}, nil)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := config.Config{
		Env:  config.EnvDevelopment,
		HTTP: config.HTTP{Addr: ":0", RequestTimeout: 5 * time.Second},
		Docs: config.Docs{Enabled: enabled},
	}

	return httpapi.NewRouter(httpapi.Dependencies{
		Config:      cfg,
		Logger:      logger,
		Procurement: service,
		Health:      health,
	})
}

func TestDocsIndex(t *testing.T) {
	h := buildDocsRouter(t, true)

	rec := doRequest(h, http.MethodGet, "/docs", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Scalar.createApiReference") {
		t.Error("body does not bootstrap Scalar")
	}
	if !strings.Contains(body, "/openapi.yaml") {
		t.Error("body does not reference the OpenAPI document")
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "cdn.jsdelivr.net") {
		t.Errorf("Content-Security-Policy = %q, want it to allow the Scalar CDN", csp)
	}
}

func TestDocsSpec(t *testing.T) {
	h := buildDocsRouter(t, true)

	rec := doRequest(h, http.MethodGet, "/openapi.yaml", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/yaml") {
		t.Errorf("Content-Type = %q, want application/yaml", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "openapi: 3.1.0") {
		t.Error("spec does not declare OpenAPI 3.1.0")
	}
	if !strings.Contains(body, "/api/v1/procurements") {
		t.Error("spec does not document the procurements endpoint")
	}
}

func TestDocsDisabled(t *testing.T) {
	h := buildDocsRouter(t, false)

	for _, path := range []string{"/docs", "/openapi.yaml"} {
		rec := doRequest(h, http.MethodGet, path, "")
		requireProblem(t, rec, http.StatusNotFound, "NOT_FOUND")
	}
}
