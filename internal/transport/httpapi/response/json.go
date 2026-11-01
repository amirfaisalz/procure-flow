package response

import (
	"encoding/json"
	"net/http"
)

// Envelope is the success response shape used by the API.
type Envelope struct {
	Data any `json:"data"`
	Meta any `json:"meta,omitempty"`
}

// JSON writes a success envelope.
func JSON(w http.ResponseWriter, status int, data any) {
	JSONWithMeta(w, status, data, nil)
}

// JSONWithMeta writes a success envelope with pagination or other metadata.
func JSONWithMeta(w http.ResponseWriter, status int, data, meta any) {
	writeJSON(w, status, Envelope{Data: data, Meta: meta})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"failed to encode response"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
