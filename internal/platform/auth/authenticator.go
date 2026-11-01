package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

// Authenticator resolves the caller identity from an HTTP request.
type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (Principal, error)
}

// Chain tries authenticators in order and returns the first success.
type Chain []Authenticator

// Authenticate implements Authenticator.
func (c Chain) Authenticate(ctx context.Context, r *http.Request) (Principal, error) {
	for _, authenticator := range c {
		if authenticator == nil {
			continue
		}
		principal, err := authenticator.Authenticate(ctx, r)
		if err == nil {
			return principal, nil
		}
		if !errors.Is(err, ErrUnauthenticated) {
			return Principal{}, err
		}
	}
	return Principal{}, ErrUnauthenticated
}

// APIKey is a static bearer token bound to a principal.
type APIKey struct {
	Token     string
	Principal Principal
}

// APIKeyAuthenticator authenticates static API keys with constant-time
// comparison. It is intended for service-to-service and local testing.
type APIKeyAuthenticator struct {
	keys []APIKey
}

// NewAPIKeyAuthenticator copies keys into a new authenticator.
func NewAPIKeyAuthenticator(keys []APIKey) *APIKeyAuthenticator {
	copied := make([]APIKey, len(keys))
	copy(copied, keys)
	return &APIKeyAuthenticator{keys: copied}
}

// Authenticate implements Authenticator.
func (a *APIKeyAuthenticator) Authenticate(_ context.Context, r *http.Request) (Principal, error) {
	token := tokenFromRequest(r, "")
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}

	for _, key := range a.keys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(key.Token)) == 1 {
			return key.Principal, nil
		}
	}
	return Principal{}, ErrUnauthenticated
}

// tokenFromRequest extracts a credential from the Authorization header, the
// X-API-Key header, or (when cookieName is set) a session cookie. An
// Authorization header that is present but malformed wins over the cookie, so
// an explicit bad credential never silently falls back to another one.
func tokenFromRequest(r *http.Request, cookieName string) string {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		const prefix = "bearer "
		if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
			return strings.TrimSpace(header[len(prefix):])
		}
		return ""
	}
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	if cookieName != "" {
		if cookie, err := r.Cookie(cookieName); err == nil {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}
