package middleware

import (
	"net/http"
)

// CORS allows exact browser origins. Wildcards are intentionally unsupported.
func CORS(allowedOrigins []string) Middleware {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Add("Vary", "Origin")

			if _, ok := allowed[origin]; !ok {
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			headers := w.Header()
			headers.Set("Access-Control-Allow-Origin", origin)
			headers.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			headers.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Request-ID, Idempotency-Key")
			headers.Set("Access-Control-Expose-Headers", "X-Request-ID, Idempotent-Replayed, Retry-After, X-RateLimit-Limit, X-RateLimit-Remaining")
			headers.Set("Access-Control-Max-Age", "600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
