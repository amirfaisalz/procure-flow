package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const issuerSecret = "0123456789abcdef0123456789abcdef"

func TestJWTIssuerRoundTrip(t *testing.T) {
	base := time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC)

	issuer := NewJWTIssuer([]byte(issuerSecret), "procure-flow", "procure-flow-api")
	issuer.now = func() time.Time { return base }

	token, expiresAt, err := issuer.Issue(
		"usr_admin", "Local Admin", "admin@procure.local",
		[]string{"ADMIN", "requester"}, "Engineering", time.Hour,
	)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if want := base.Add(time.Hour); !expiresAt.Equal(want) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, want)
	}

	authenticator := NewJWTAuthenticator(JWTConfig{
		Secret:     []byte(issuerSecret),
		Issuer:     "procure-flow",
		Audience:   "procure-flow-api",
		CookieName: "procure_session",
	})
	authenticator.now = func() time.Time { return base.Add(time.Minute) }

	t.Run("bearer header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		principal, err := authenticator.Authenticate(context.Background(), req)
		if err != nil {
			t.Fatalf("Authenticate() error = %v", err)
		}
		if principal.Subject != "usr_admin" {
			t.Errorf("Subject = %q, want usr_admin", principal.Subject)
		}
		if principal.Email != "admin@procure.local" {
			t.Errorf("Email = %q, want admin@procure.local", principal.Email)
		}
		if principal.Name != "Local Admin" {
			t.Errorf("Name = %q, want Local Admin", principal.Name)
		}
		if principal.Department != "Engineering" {
			t.Errorf("Department = %q, want Engineering", principal.Department)
		}
		if len(principal.Roles) != 2 || !principal.HasRole(RoleAdmin) || !principal.HasRole(RoleRequester) {
			t.Errorf("Roles = %v, want [admin requester]", principal.Roles)
		}
	})

	t.Run("session cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
		req.AddCookie(&http.Cookie{Name: "procure_session", Value: token})

		principal, err := authenticator.Authenticate(context.Background(), req)
		if err != nil {
			t.Fatalf("Authenticate() error = %v", err)
		}
		if principal.Subject != "usr_admin" {
			t.Errorf("Subject = %q, want usr_admin", principal.Subject)
		}
	})

	t.Run("header wins over cookie", func(t *testing.T) {
		other, _, err := issuer.Issue("usr_other", "Other", "other@procure.local", []string{"viewer"}, "", time.Hour)
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
		req.AddCookie(&http.Cookie{Name: "procure_session", Value: token})
		req.Header.Set("Authorization", "Bearer "+other)

		principal, err := authenticator.Authenticate(context.Background(), req)
		if err != nil {
			t.Fatalf("Authenticate() error = %v", err)
		}
		if principal.Subject != "usr_other" {
			t.Errorf("Subject = %q, want usr_other (header must win)", principal.Subject)
		}
	})

	t.Run("tampered token", func(t *testing.T) {
		tampered := token[:len(token)-2] + "AA"
		req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
		req.Header.Set("Authorization", "Bearer "+tampered)

		if _, err := authenticator.Authenticate(context.Background(), req); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("Authenticate(tampered) error = %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		authenticator.now = func() time.Time { return base.Add(2 * time.Hour) }
		t.Cleanup(func() { authenticator.now = func() time.Time { return base.Add(time.Minute) } })

		req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		if _, err := authenticator.Authenticate(context.Background(), req); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("Authenticate(expired) error = %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("wrong audience", func(t *testing.T) {
		other := NewJWTAuthenticator(JWTConfig{
			Secret:     []byte(issuerSecret),
			Issuer:     "procure-flow",
			Audience:   "someone-else",
			CookieName: "procure_session",
		})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		if _, err := other.Authenticate(context.Background(), req); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("Authenticate(wrong audience) error = %v, want ErrUnauthenticated", err)
		}
	})
}

func TestJWTIssuerRejectsInvalidInput(t *testing.T) {
	issuer := NewJWTIssuer([]byte(issuerSecret), "procure-flow", "procure-flow-api")

	if _, _, err := issuer.Issue("usr_1", "A", "a@procure.local", []string{"admin"}, "", 0); err == nil {
		t.Error("Issue(ttl=0) error = nil, want failure")
	}
	if _, _, err := issuer.Issue("", "A", "a@procure.local", []string{"admin"}, "", time.Hour); err == nil {
		t.Error("Issue(no subject) error = nil, want failure")
	}
	if _, _, err := issuer.Issue("usr_1", "A", "a@procure.local", []string{"root"}, "", time.Hour); err == nil {
		t.Error("Issue(unknown role) error = nil, want failure")
	}
}

func TestJWTAuthenticatorIgnoresCookieWhenDisabled(t *testing.T) {
	issuer := NewJWTIssuer([]byte(issuerSecret), "procure-flow", "procure-flow-api")
	token, _, err := issuer.Issue("usr_1", "A", "a@procure.local", []string{"viewer"}, "", time.Hour)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	authenticator := NewJWTAuthenticator(JWTConfig{
		Secret:   []byte(issuerSecret),
		Issuer:   "procure-flow",
		Audience: "procure-flow-api",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/procurements", nil)
	req.AddCookie(&http.Cookie{Name: "procure_session", Value: token})

	if _, err := authenticator.Authenticate(context.Background(), req); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Authenticate(cookie) error = %v, want ErrUnauthenticated", err)
	}
}

func TestJWTIssuerTokensAreDetached(t *testing.T) {
	issuer := NewJWTIssuer([]byte(issuerSecret), "procure-flow", "procure-flow-api")
	token, _, err := issuer.Issue("usr_1", "A", "a@procure.local", []string{"admin"}, "", time.Hour)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if parts := strings.Split(token, "."); len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}
}
