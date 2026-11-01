package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/config"
)

func TestLoadRedisDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(env(nil))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Redis.URL != "" {
		t.Errorf("Redis.URL = %q, want empty", cfg.Redis.URL)
	}
	if cfg.Redis.CacheEnabled {
		t.Error("Redis.CacheEnabled = true, want false when REDIS_URL is unset")
	}
	if cfg.Redis.CacheTTL != 30*time.Second {
		t.Errorf("Redis.CacheTTL = %v, want 30s", cfg.Redis.CacheTTL)
	}
	if cfg.RateLimit.Store != config.RateLimitStoreAuto {
		t.Errorf("RateLimit.Store = %q, want auto", cfg.RateLimit.Store)
	}
}

func TestLoadRedisEnabledByURL(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"REDIS_URL": "redis://127.0.0.1:6379/0",
		"CACHE_TTL": "45s",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Redis.URL != "redis://127.0.0.1:6379/0" {
		t.Errorf("Redis.URL = %q", cfg.Redis.URL)
	}
	if !cfg.Redis.CacheEnabled {
		t.Error("Redis.CacheEnabled = false, want true when REDIS_URL is set")
	}
	if cfg.Redis.CacheTTL != 45*time.Second {
		t.Errorf("Redis.CacheTTL = %v, want 45s", cfg.Redis.CacheTTL)
	}
}

func TestLoadRedisInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "cache without URL",
			env:  map[string]string{"CACHE_ENABLED": "true"},
			want: "CACHE_ENABLED",
		},
		{
			name: "unsupported scheme",
			env:  map[string]string{"REDIS_URL": "http://127.0.0.1:6379"},
			want: "REDIS_URL",
		},
		{
			name: "missing host",
			env:  map[string]string{"REDIS_URL": "redis://"},
			want: "REDIS_URL",
		},
		{
			name: "invalid TTL",
			env:  map[string]string{"REDIS_URL": "redis://localhost:6379", "CACHE_TTL": "soon"},
			want: "CACHE_TTL",
		},
		{
			name: "redis rate limit store without URL",
			env:  map[string]string{"RATE_LIMIT_ENABLED": "true", "RATE_LIMIT_STORE": "redis"},
			want: "RATE_LIMIT_STORE",
		},
		{
			name: "unknown rate limit store",
			env:  map[string]string{"RATE_LIMIT_STORE": "memcached"},
			want: "RATE_LIMIT_STORE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadFrom(env(tt.env))
			if err == nil {
				t.Fatal("LoadFrom() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %s", err, tt.want)
			}
		})
	}
}

func TestLoadRedisRateLimitStore(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"REDIS_URL":          "redis://127.0.0.1:6379",
		"RATE_LIMIT_ENABLED": "true",
		"RATE_LIMIT_STORE":   "redis",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.RateLimit.Store != config.RateLimitStoreRedis {
		t.Errorf("RateLimit.Store = %q, want redis", cfg.RateLimit.Store)
	}
}
