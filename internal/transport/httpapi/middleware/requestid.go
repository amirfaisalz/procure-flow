package middleware

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
)

// RequestIDHeader is the canonical request correlation header.
const RequestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// RequestID accepts a valid incoming correlation ID or generates a new one.
// The ID is stored in the request context and echoed in the response.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimSpace(r.Header.Get(RequestIDHeader))
			if !validRequestID.MatchString(id) {
				id = requestid.New()
			}

			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
		})
	}
}
