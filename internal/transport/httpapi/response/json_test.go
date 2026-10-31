package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

func TestJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	response.JSON(rec, http.StatusCreated, map[string]string{"id": "prc_1"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Data["id"] != "prc_1" {
		t.Errorf("data.id = %q, want prc_1", body.Data["id"])
	}
}

func TestJSONWithMeta(t *testing.T) {
	rec := httptest.NewRecorder()

	response.JSONWithMeta(rec, http.StatusOK, []string{"a"}, map[string]int{"total": 1})

	var body struct {
		Data []string       `json:"data"`
		Meta map[string]int `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0] != "a" {
		t.Errorf("data = %v, want [a]", body.Data)
	}
	if body.Meta["total"] != 1 {
		t.Errorf("meta.total = %d, want 1", body.Meta["total"])
	}
}

func TestNoContent(t *testing.T) {
	rec := httptest.NewRecorder()

	response.NoContent(rec)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}

func TestWriteProblem(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/procurements", nil)
	req = req.WithContext(requestid.With(req.Context(), "req-123"))
	rec := httptest.NewRecorder()

	response.WriteProblem(rec, req, response.Problem{
		Status: http.StatusUnprocessableEntity,
		Code:   "VALIDATION_ERROR",
		Title:  "Validation failed",
		Detail: "one or more fields are invalid",
		Errors: map[string]string{"title": "is required"},
	})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}

	var body response.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Type != "about:blank" {
		t.Errorf("type = %q, want about:blank", body.Type)
	}
	if body.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", body.Status)
	}
	if body.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", body.Code)
	}
	if body.Instance != "/api/v1/procurements" {
		t.Errorf("instance = %q, want request path", body.Instance)
	}
	if body.RequestID != "req-123" {
		t.Errorf("request_id = %q, want req-123", body.RequestID)
	}
	if body.Errors["title"] != "is required" {
		t.Errorf("errors.title = %q, want is required", body.Errors["title"])
	}
}
