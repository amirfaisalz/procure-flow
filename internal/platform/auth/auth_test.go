package auth_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
)

const secret = "0123456789abcdef0123456789abcdef"

func requestWith(headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req
}

func signToken(t *testing.T, key string, claims map[string]any) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	body := header + "." + base64.RawURLEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validClaims(now time.Time) map[string]any {
	return map[string]any{
		"sub":        "user_amir",
		"name":       "Amir",
		"roles":      []string{"requester", "viewer"},
		"department": "Engineering",
		"iss":        "procure-flow",
		"aud":        "procure-flow-api",
		"exp":        now.Add(time.Hour).Unix(),
		"iat":        now.Unix(),
	}
}

func TestAPIKeyAuthenticator(t *testing.T) {
	authenticator := auth.NewAPIKeyAuthenticator([]auth.APIKey{
		{
			Token: "test-api-key-0123456789",
			Principal: auth.Principal{
				Subject:    "user_admin",
				Name:       "Admin",
				Roles:      []auth.Role{auth.RoleAdmin},
				Department: "",
			},
		},
	})

	tests := []struct {
		name    string
		headers map[string]string
		wantSub string
		wantErr bool
	}{
		{
			name:    "bearer header",
			headers: map[string]string{"Authorization": "Bearer test-api-key-0123456789"},
			wantSub: "user_admin",
		},
		{
			name:    "lowercase bearer scheme",
			headers: map[string]string{"Authorization": "bearer test-api-key-0123456789"},
			wantSub: "user_admin",
		},
		{
			name:    "x-api-key header",
			headers: map[string]string{"X-API-Key": "test-api-key-0123456789"},
			wantSub: "user_admin",
		},
		{
			name:    "unknown token",
			headers: map[string]string{"Authorization": "Bearer nope"},
			wantErr: true,
		},
		{
			name:    "missing token",
			headers: nil,
			wantErr: true,
		},
		{
			name:    "non-bearer authorization",
			headers: map[string]string{"Authorization": "Basic dXNlcjpwYXNz"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principal, err := authenticator.Authenticate(context.Background(), requestWith(tt.headers))
			if tt.wantErr {
				if !errors.Is(err, auth.ErrUnauthenticated) {
					t.Fatalf("error = %v, want ErrUnauthenticated", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
			if principal.Subject != tt.wantSub {
				t.Errorf("Subject = %q, want %q", principal.Subject, tt.wantSub)
			}
		})
	}
}

func TestJWTAuthenticatorValid(t *testing.T) {
	now := time.Now()
	authenticator := auth.NewJWTAuthenticator(auth.JWTConfig{
		Secret:   []byte(secret),
		Issuer:   "procure-flow",
		Audience: "procure-flow-api",
		Leeway:   30 * time.Second,
	})

	token := signToken(t, secret, validClaims(now))
	principal, err := authenticator.Authenticate(context.Background(), requestWith(map[string]string{
		"Authorization": "Bearer " + token,
	}))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if principal.Subject != "user_amir" || principal.Name != "Amir" || principal.Department != "Engineering" {
		t.Errorf("principal = %+v, want subject/name/department", principal)
	}
	if !principal.HasRole(auth.RoleRequester) || principal.HasRole(auth.RoleAdmin) {
		t.Errorf("roles = %v, want requester but not admin", principal.Roles)
	}
}

func TestJWTAuthenticatorRejects(t *testing.T) {
	now := time.Now()
	authenticator := auth.NewJWTAuthenticator(auth.JWTConfig{
		Secret:   []byte(secret),
		Issuer:   "procure-flow",
		Audience: "procure-flow-api",
	})

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "expired", mutate: func(c map[string]any) { c["exp"] = now.Add(-time.Hour).Unix() }},
		{name: "missing exp", mutate: func(c map[string]any) { delete(c, "exp") }},
		{name: "not yet valid", mutate: func(c map[string]any) { c["nbf"] = now.Add(time.Hour).Unix() }},
		{name: "wrong issuer", mutate: func(c map[string]any) { c["iss"] = "someone-else" }},
		{name: "wrong audience", mutate: func(c map[string]any) { c["aud"] = "another-api" }},
		{name: "missing subject", mutate: func(c map[string]any) { delete(c, "sub") }},
		{name: "invalid role", mutate: func(c map[string]any) { c["roles"] = []string{"superuser"} }},
		{name: "no roles", mutate: func(c map[string]any) { c["roles"] = []string{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := validClaims(now)
			tt.mutate(claims)
			token := signToken(t, secret, claims)

			_, err := authenticator.Authenticate(context.Background(), requestWith(map[string]string{
				"Authorization": "Bearer " + token,
			}))
			if !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("error = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestJWTAuthenticatorRejectsBadSignature(t *testing.T) {
	authenticator := auth.NewJWTAuthenticator(auth.JWTConfig{
		Secret:   []byte(secret),
		Issuer:   "procure-flow",
		Audience: "procure-flow-api",
	})

	token := signToken(t, "another-secret-another-secret-32", validClaims(time.Now()))
	_, err := authenticator.Authenticate(context.Background(), requestWith(map[string]string{
		"Authorization": "Bearer " + token,
	}))
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestJWTAuthenticatorRejectsMalformed(t *testing.T) {
	authenticator := auth.NewJWTAuthenticator(auth.JWTConfig{
		Secret:   []byte(secret),
		Issuer:   "procure-flow",
		Audience: "procure-flow-api",
	})

	for _, token := range []string{"", "not-a-jwt", "a.b", "a.b.c.d", "!!!.???.***"} {
		t.Run(token, func(t *testing.T) {
			_, err := authenticator.Authenticate(context.Background(), requestWith(map[string]string{
				"Authorization": "Bearer " + token,
			}))
			if !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("error = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestJWTAuthenticatorRejectsAlgNone(t *testing.T) {
	authenticator := auth.NewJWTAuthenticator(auth.JWTConfig{
		Secret:   []byte(secret),
		Issuer:   "procure-flow",
		Audience: "procure-flow-api",
	})

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(validClaims(time.Now()))
	token := header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."

	_, err := authenticator.Authenticate(context.Background(), requestWith(map[string]string{
		"Authorization": "Bearer " + token,
	}))
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestChainFallsBack(t *testing.T) {
	now := time.Now()
	chain := auth.Chain{
		auth.NewAPIKeyAuthenticator(nil),
		auth.NewJWTAuthenticator(auth.JWTConfig{
			Secret:   []byte(secret),
			Issuer:   "procure-flow",
			Audience: "procure-flow-api",
		}),
	}

	token := signToken(t, secret, validClaims(now))
	principal, err := chain.Authenticate(context.Background(), requestWith(map[string]string{
		"Authorization": "Bearer " + token,
	}))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.Subject != "user_amir" {
		t.Errorf("Subject = %q, want user_amir", principal.Subject)
	}
}

func TestParseRoles(t *testing.T) {
	roles, err := auth.ParseRoles([]string{"admin", "APPROVER", " requester "})
	if err != nil {
		t.Fatalf("ParseRoles() error = %v", err)
	}
	if len(roles) != 3 {
		t.Fatalf("len(roles) = %d, want 3", len(roles))
	}

	if _, err := auth.ParseRoles([]string{"root"}); err == nil {
		t.Fatal("ParseRoles() error = nil, want error for unknown role")
	}
	if _, err := auth.ParseRoles(nil); err == nil {
		t.Fatal("ParseRoles() error = nil, want error for empty roles")
	}
}
