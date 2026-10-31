package middleware

import (
	"net/http"
)

// SecurityHeaders applies conservative defaults for a JSON API.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers := w.Header()
			headers.Set("X-Content-Type-Options", "nosniff")
			headers.Set("X-Frame-Options", "DENY")
			headers.Set("Referrer-Policy", "no-referrer")
			headers.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			headers.Set("Cache-Control", "no-store")
			if r.TLS != nil {
				headers.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}
