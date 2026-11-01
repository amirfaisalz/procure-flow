package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
)

const testAPIKeys = `[
  {"token":"test-admin-token-0123456789","subject":"user_admin","name":"Admin","roles":["admin"],"department":""},
  {"token":"test-approver-token-0123456789","subject":"user_approver","name":"Approver","roles":["approver"],"department":"Finance"}
]`

func TestLoadAuthDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(env(nil))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Auth.Enabled {
		t.Error("Auth.Enabled = true in development, want false")
	}
	if cfg.Auth.JWTIssuer != "procure-flow" || cfg.Auth.JWTAudience != "procure-flow-api" {
		t.Errorf("JWT issuer/audience = %q/%q, want defaults", cfg.Auth.JWTIssuer, cfg.Auth.JWTAudience)
	}
	if cfg.Auth.JWTLeeway != 30*time.Second {
		t.Errorf("JWTLeeway = %v, want 30s", cfg.Auth.JWTLeeway)
	}
}

func TestLoadAuthEnabled(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"AUTH_ENABLED":      "true",
		"AUTH_API_KEYS":     testAPIKeys,
		"AUTH_JWT_SECRET":   "0123456789abcdef0123456789abcdef",
		"AUTH_JWT_ISSUER":   "issuer",
		"AUTH_JWT_AUDIENCE": "audience",
		"AUTH_JWT_LEEWAY":   "10s",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	authCfg := cfg.Auth
	if !authCfg.Enabled {
		t.Fatal("Auth.Enabled = false, want true")
	}
	if len(authCfg.APIKeys) != 2 {
		t.Fatalf("len(APIKeys) = %d, want 2", len(authCfg.APIKeys))
	}
	if authCfg.APIKeys[0].Subject != "user_admin" {
		t.Errorf("first subject = %q, want user_admin", authCfg.APIKeys[0].Subject)
	}
	if !authCfg.APIKeys[1].Roles[0].Valid() || authCfg.APIKeys[1].Roles[0] != auth.RoleApprover {
		t.Errorf("second roles = %v, want approver", authCfg.APIKeys[1].Roles)
	}
	if authCfg.APIKeys[1].Department != "Finance" {
		t.Errorf("department = %q, want Finance", authCfg.APIKeys[1].Department)
	}
	if authCfg.JWTIssuer != "issuer" || authCfg.JWTAudience != "audience" {
		t.Errorf("issuer/audience = %q/%q", authCfg.JWTIssuer, authCfg.JWTAudience)
	}
	if authCfg.JWTLeeway != 10*time.Second {
		t.Errorf("JWTLeeway = %v, want 10s", authCfg.JWTLeeway)
	}
}

func TestLoadAuthRequiresCredentialsWhenEnabled(t *testing.T) {
	_, err := config.LoadFrom(env(map[string]string{"AUTH_ENABLED": "true"}))
	if err == nil {
		t.Fatal("LoadFrom() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "AUTH_API_KEYS") {
		t.Errorf("error = %q, want it to mention AUTH_API_KEYS", err)
	}
}

func TestLoadAuthProductionRequiresAuth(t *testing.T) {
	_, err := config.LoadFrom(env(map[string]string{
		"APP_ENV":      "production",
		"DATABASE_URL": "postgres://localhost/procure",
		"AUTH_ENABLED": "false",
	}))
	if err == nil {
		t.Fatal("LoadFrom() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "AUTH_ENABLED") {
		t.Errorf("error = %q, want it to mention AUTH_ENABLED", err)
	}
}

func TestLoadAuthInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "invalid json",
			env:  map[string]string{"AUTH_ENABLED": "true", "AUTH_API_KEYS": "{"},
			want: "AUTH_API_KEYS",
		},
		{
			name: "unknown role",
			env: map[string]string{
				"AUTH_ENABLED":  "true",
				"AUTH_API_KEYS": `[{"token":"test-token-0123456789","subject":"u","roles":["root"]}]`,
			},
			want: "role",
		},
		{
			name: "short token",
			env: map[string]string{
				"AUTH_ENABLED":  "true",
				"AUTH_API_KEYS": `[{"token":"short","subject":"u","roles":["admin"]}]`,
			},
			want: "token",
		},
		{
			name: "missing subject",
			env: map[string]string{
				"AUTH_ENABLED":  "true",
				"AUTH_API_KEYS": `[{"token":"test-token-0123456789","roles":["admin"]}]`,
			},
			want: "subject",
		},
		{
			name: "duplicate token",
			env: map[string]string{
				"AUTH_ENABLED": "true",
				"AUTH_API_KEYS": `[
					{"token":"test-token-0123456789","subject":"a","roles":["admin"]},
					{"token":"test-token-0123456789","subject":"b","roles":["viewer"]}
				]`,
			},
			want: "duplicate",
		},
		{
			name: "short jwt secret",
			env:  map[string]string{"AUTH_ENABLED": "true", "AUTH_JWT_SECRET": "too-short"},
			want: "AUTH_JWT_SECRET",
		},
		{
			name: "invalid leeway",
			env: map[string]string{
				"AUTH_ENABLED":    "true",
				"AUTH_API_KEYS":   testAPIKeys,
				"AUTH_JWT_LEEWAY": "soon",
			},
			want: "AUTH_JWT_LEEWAY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadFrom(env(tt.env))
			if err == nil {
				t.Fatal("LoadFrom() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}
