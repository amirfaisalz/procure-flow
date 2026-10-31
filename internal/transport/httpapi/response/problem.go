package response

import (
	"encoding/json"
	"net/http"

	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
)

// Problem follows RFC 9457 (application/problem+json) with API extensions.
type Problem struct {
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Status    int               `json:"status"`
	Detail    string            `json:"detail,omitempty"`
	Instance  string            `json:"instance,omitempty"`
	Code      string            `json:"code,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Errors    map[string]string `json:"errors,omitempty"`
}

// NewProblem builds a problem document with the common fields set.
func NewProblem(status int, code, title, detail string) Problem {
	return Problem{
		Status: status,
		Code:   code,
		Title:  title,
		Detail: detail,
	}
}

// WriteProblem writes an RFC 9457 problem document.
func WriteProblem(w http.ResponseWriter, r *http.Request, problem Problem) {
	if problem.Type == "" {
		problem.Type = "about:blank"
	}
	if problem.Status == 0 {
		problem.Status = http.StatusInternalServerError
	}
	if problem.Title == "" {
		problem.Title = http.StatusText(problem.Status)
	}
	if problem.Instance == "" {
		problem.Instance = r.URL.Path
	}
	if problem.RequestID == "" {
		problem.RequestID = requestid.From(r.Context())
	}

	body, err := json.Marshal(problem)
	if err != nil {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"failed to encode error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(problem.Status)
	_, _ = w.Write(append(body, '\n'))
}
