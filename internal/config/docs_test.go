package config_test

import (
	"strings"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/config"
)

func TestLoadDocsDefault(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "development", env: nil, want: true},
		{name: "staging", env: map[string]string{"APP_ENV": "staging"}, want: true},
		{name: "production", env: map[string]string{"APP_ENV": "production"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadFrom(env(tt.env))
			if err != nil {
				t.Fatalf("LoadFrom() error = %v", err)
			}
			if cfg.Docs.Enabled != tt.want {
				t.Errorf("Docs.Enabled = %v, want %v", cfg.Docs.Enabled, tt.want)
			}
		})
	}
}

func TestLoadDocsOverride(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{
			name: "enabled in production",
			env:  map[string]string{"APP_ENV": "production", "DOCS_ENABLED": "true"},
			want: true,
		},
		{
			name: "disabled in development",
			env:  map[string]string{"DOCS_ENABLED": "false"},
			want: false,
		},
		{
			name: "uppercase boolean",
			env:  map[string]string{"DOCS_ENABLED": "TRUE"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadFrom(env(tt.env))
			if err != nil {
				t.Fatalf("LoadFrom() error = %v", err)
			}
			if cfg.Docs.Enabled != tt.want {
				t.Errorf("Docs.Enabled = %v, want %v", cfg.Docs.Enabled, tt.want)
			}
		})
	}
}

func TestLoadDocsInvalid(t *testing.T) {
	_, err := config.LoadFrom(env(map[string]string{"DOCS_ENABLED": "sometimes"}))
	if err == nil {
		t.Fatal("LoadFrom() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "DOCS_ENABLED") {
		t.Errorf("error = %q, want it to mention DOCS_ENABLED", err)
	}
}
