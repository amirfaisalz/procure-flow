package response_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

type decodePayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func decodeRequest(body, contentType string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return httptest.NewRecorder(), req
}

func requireDecodeError(t *testing.T, err error, status int, code string) {
	t.Helper()

	var decodeErr *response.DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("error = %v, want *response.DecodeError", err)
	}
	if decodeErr.Status != status {
		t.Errorf("status = %d, want %d", decodeErr.Status, status)
	}
	if decodeErr.Code != code {
		t.Errorf("code = %q, want %q", decodeErr.Code, code)
	}
}

func TestDecodeJSON(t *testing.T) {
	rec, req := decodeRequest(`{"name":"laptop","count":2}`, "application/json; charset=utf-8")

	var got decodePayload
	if err := response.DecodeJSON(rec, req, &got); err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	if got.Name != "laptop" || got.Count != 2 {
		t.Errorf("decoded = %+v, want {laptop 2}", got)
	}
}

func TestDecodeJSONRequiresContentType(t *testing.T) {
	rec, req := decodeRequest(`{"name":"laptop"}`, "")

	var got decodePayload
	err := response.DecodeJSON(rec, req, &got)
	requireDecodeError(t, err, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
}

func TestDecodeJSONRejectsWrongContentType(t *testing.T) {
	rec, req := decodeRequest(`{"name":"laptop"}`, "text/plain")

	var got decodePayload
	err := response.DecodeJSON(rec, req, &got)
	requireDecodeError(t, err, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	rec, req := decodeRequest(`{"name":"laptop","unknown":true}`, "application/json")

	var got decodePayload
	err := response.DecodeJSON(rec, req, &got)
	requireDecodeError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestDecodeJSONRejectsEmptyBody(t *testing.T) {
	rec, req := decodeRequest("", "application/json")

	var got decodePayload
	err := response.DecodeJSON(rec, req, &got)
	requireDecodeError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestDecodeJSONRejectsTrailingData(t *testing.T) {
	rec, req := decodeRequest(`{"name":"laptop"} {"name":"extra"}`, "application/json")

	var got decodePayload
	err := response.DecodeJSON(rec, req, &got)
	requireDecodeError(t, err, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	body := `{"name":"` + strings.Repeat("a", int(response.DefaultMaxBodyBytes)+1) + `"}`
	rec, req := decodeRequest(body, "application/json")

	var got decodePayload
	err := response.DecodeJSON(rec, req, &got)
	requireDecodeError(t, err, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
}
