package response

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

// DefaultMaxBodyBytes is the request body limit applied by DecodeJSON.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// DecodeError describes a request decoding failure that maps to an HTTP status.
type DecodeError struct {
	Status  int
	Code    string
	Message string
}

func (e *DecodeError) Error() string {
	return e.Message
}

// DecodeJSON strictly decodes a single JSON object with a size limit and
// unknown-field rejection. It requires an application/json Content-Type.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request, dst *T) error {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return &DecodeError{
			Status:  http.StatusUnsupportedMediaType,
			Code:    "UNSUPPORTED_MEDIA_TYPE",
			Message: "Content-Type must be application/json",
		}
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return &DecodeError{
			Status:  http.StatusUnsupportedMediaType,
			Code:    "UNSUPPORTED_MEDIA_TYPE",
			Message: "Content-Type must be application/json",
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, DefaultMaxBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return &DecodeError{
				Status:  http.StatusRequestEntityTooLarge,
				Code:    "PAYLOAD_TOO_LARGE",
				Message: fmt.Sprintf("request body must not exceed %d bytes", DefaultMaxBodyBytes),
			}
		}
		if errors.Is(err, io.EOF) {
			return &DecodeError{
				Status:  http.StatusBadRequest,
				Code:    "INVALID_REQUEST",
				Message: "request body must not be empty",
			}
		}
		return &DecodeError{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_REQUEST",
			Message: "invalid JSON payload: " + err.Error(),
		}
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &DecodeError{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_REQUEST",
			Message: "request body must contain a single JSON object",
		}
	}

	return nil
}
