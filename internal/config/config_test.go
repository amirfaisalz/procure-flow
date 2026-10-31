package config_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/config"
)

func env(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(env(nil))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.Env != config.EnvDevelopment {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want INFO", cfg.LogLevel)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("HTTP.Addr = %q, want :8080", cfg.HTTP.Addr)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 5s", cfg.HTTP.ReadHeaderTimeout)
	}
	if cfg.HTTP.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %v, want 15s", cfg.HTTP.ReadTimeout)
	}
	if cfg.HTTP.WriteTimeout != 30*time.Second {
		t.Errorf("WriteTimeout = %v, want 30s", cfg.HTTP.WriteTimeout)
	}
	if cfg.HTTP.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", cfg.HTTP.IdleTimeout)
	}
	if cfg.HTTP.RequestTimeout != 20*time.Second {
		t.Errorf("RequestTimeout = %v, want 20s", cfg.HTTP.RequestTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.HTTP.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want %d", cfg.HTTP.MaxHeaderBytes, 1<<20)
	}
	if len(cfg.CORS.AllowedOrigins) != 0 {
		t.Errorf("CORS.AllowedOrigins = %v, want empty", cfg.CORS.AllowedOrigins)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"APP_ENV":                  "production",
		"LOG_LEVEL":                "debug",
		"HTTP_ADDR":                "127.0.0.1:9090",
		"HTTP_READ_HEADER_TIMEOUT": "2s",
		"HTTP_READ_TIMEOUT":        "10s",
		"HTTP_WRITE_TIMEOUT":       "40s",
		"HTTP_IDLE_TIMEOUT":        "90s",
		"HTTP_REQUEST_TIMEOUT":     "25s",
		"HTTP_SHUTDOWN_TIMEOUT":    "5s",
		"HTTP_MAX_HEADER_BYTES":    "2048",
		"CORS_ALLOWED_ORIGINS":     " https://app.example.com , http://localhost:3000 ",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.Env != config.EnvProduction || !cfg.Env.IsProduction() {
		t.Errorf("Env = %q, want production", cfg.Env)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want DEBUG", cfg.LogLevel)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9090" {
		t.Errorf("HTTP.Addr = %q, want 127.0.0.1:9090", cfg.HTTP.Addr)
	}
	if cfg.HTTP.ReadHeaderTimeout != 2*time.Second ||
		cfg.HTTP.ReadTimeout != 10*time.Second ||
		cfg.HTTP.WriteTimeout != 40*time.Second ||
		cfg.HTTP.IdleTimeout != 90*time.Second ||
		cfg.HTTP.RequestTimeout != 25*time.Second ||
		cfg.HTTP.ShutdownTimeout != 5*time.Second {
		t.Errorf("HTTP durations were not loaded: %+v", cfg.HTTP)
	}
	if cfg.HTTP.MaxHeaderBytes != 2048 {
		t.Errorf("MaxHeaderBytes = %d, want 2048", cfg.HTTP.MaxHeaderBytes)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 {
		t.Fatalf("CORS.AllowedOrigins = %v, want 2 entries", cfg.CORS.AllowedOrigins)
	}
	if cfg.CORS.AllowedOrigins[0] != "https://app.example.com" || cfg.CORS.AllowedOrigins[1] != "http://localhost:3000" {
		t.Errorf("CORS.AllowedOrigins = %v, want trimmed values", cfg.CORS.AllowedOrigins)
	}
}

func TestLoadInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{
			name:   "unknown environment",
			values: map[string]string{"APP_ENV": "qa"},
			want:   "APP_ENV",
		},
		{
			name:   "unknown log level",
			values: map[string]string{"LOG_LEVEL": "verbose"},
			want:   "LOG_LEVEL",
		},
		{
			name:   "invalid duration",
			values: map[string]string{"HTTP_READ_TIMEOUT": "soon"},
			want:   "HTTP_READ_TIMEOUT",
		},
		{
			name:   "non-positive duration",
			values: map[string]string{"HTTP_READ_TIMEOUT": "-1s"},
			want:   "HTTP_READ_TIMEOUT",
		},
		{
			name:   "invalid max header bytes",
			values: map[string]string{"HTTP_MAX_HEADER_BYTES": "big"},
			want:   "HTTP_MAX_HEADER_BYTES",
		},
		{
			name:   "non-positive max header bytes",
			values: map[string]string{"HTTP_MAX_HEADER_BYTES": "0"},
			want:   "HTTP_MAX_HEADER_BYTES",
		},
		{
			name:   "request timeout exceeds write timeout",
			values: map[string]string{"HTTP_REQUEST_TIMEOUT": "40s"},
			want:   "HTTP_REQUEST_TIMEOUT",
		},
		{
			name:   "cors wildcard",
			values: map[string]string{"CORS_ALLOWED_ORIGINS": "*"},
			want:   "CORS_ALLOWED_ORIGINS",
		},
		{
			name:   "cors relative url",
			values: map[string]string{"CORS_ALLOWED_ORIGINS": "not-a-url"},
			want:   "CORS_ALLOWED_ORIGINS",
		},
		{
			name:   "cors url with path",
			values: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com/api"},
			want:   "CORS_ALLOWED_ORIGINS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadFrom(env(tt.values))
			if err == nil {
				t.Fatal("LoadFrom() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}
