package config_test

import (
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/config"
)

func TestLoadIdempotencyDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(env(nil))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if !cfg.Idempotency.Enabled {
		t.Error("Idempotency.Enabled = false, want true")
	}
	if cfg.Idempotency.TTL != 24*time.Hour {
		t.Errorf("Idempotency.TTL = %v, want 24h", cfg.Idempotency.TTL)
	}
}

func TestLoadIdempotencyOverrides(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"IDEMPOTENCY_ENABLED": "false",
		"IDEMPOTENCY_TTL":     "2h",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Idempotency.Enabled {
		t.Error("Idempotency.Enabled = true, want false")
	}
	if cfg.Idempotency.TTL != 2*time.Hour {
		t.Errorf("Idempotency.TTL = %v, want 2h", cfg.Idempotency.TTL)
	}
}

func TestLoadIdempotencyRejectsInvalidTTL(t *testing.T) {
	tests := []string{"nope", "0s", "-1h"}
	for _, ttl := range tests {
		t.Run(ttl, func(t *testing.T) {
			if _, err := config.LoadFrom(env(map[string]string{"IDEMPOTENCY_TTL": ttl})); err == nil {
				t.Fatalf("LoadFrom() with IDEMPOTENCY_TTL=%q succeeded, want an error", ttl)
			}
		})
	}
}

func TestLoadRateLimitDefaults(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		enabled bool
	}{
		{name: "development", env: nil, enabled: false},
		{name: "staging", env: map[string]string{"APP_ENV": "staging", "AUTH_API_KEYS": testAPIKeys}, enabled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadFrom(env(tt.env))
			if err != nil {
				t.Fatalf("LoadFrom() error = %v", err)
			}
			if cfg.RateLimit.Enabled != tt.enabled {
				t.Errorf("RateLimit.Enabled = %v, want %v", cfg.RateLimit.Enabled, tt.enabled)
			}
			if cfg.RateLimit.RequestsPerSecond != 20 {
				t.Errorf("RateLimit.RequestsPerSecond = %v, want 20", cfg.RateLimit.RequestsPerSecond)
			}
			if cfg.RateLimit.Burst != 40 {
				t.Errorf("RateLimit.Burst = %d, want 40", cfg.RateLimit.Burst)
			}
		})
	}
}

func TestLoadRateLimitOverrides(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"RATE_LIMIT_ENABLED": "true",
		"RATE_LIMIT_RPS":     "2.5",
		"RATE_LIMIT_BURST":   "7",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if !cfg.RateLimit.Enabled {
		t.Error("RateLimit.Enabled = false, want true")
	}
	if cfg.RateLimit.RequestsPerSecond != 2.5 {
		t.Errorf("RateLimit.RequestsPerSecond = %v, want 2.5", cfg.RateLimit.RequestsPerSecond)
	}
	if cfg.RateLimit.Burst != 7 {
		t.Errorf("RateLimit.Burst = %d, want 7", cfg.RateLimit.Burst)
	}
}

func TestLoadRateLimitRejectsInvalidValues(t *testing.T) {
	tests := []map[string]string{
		{"RATE_LIMIT_RPS": "0"},
		{"RATE_LIMIT_RPS": "-1"},
		{"RATE_LIMIT_RPS": "fast"},
		{"RATE_LIMIT_BURST": "0"},
		{"RATE_LIMIT_BURST": "-3"},
	}
	for _, values := range tests {
		if _, err := config.LoadFrom(env(values)); err == nil {
			t.Errorf("LoadFrom() with %v succeeded, want an error", values)
		}
	}
}

func TestLoadMetricsDefaultAndOverride(t *testing.T) {
	cfg, err := config.LoadFrom(env(nil))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if !cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = false, want true")
	}

	cfg, err = config.LoadFrom(env(map[string]string{"METRICS_ENABLED": "false"}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = true, want false")
	}
}
