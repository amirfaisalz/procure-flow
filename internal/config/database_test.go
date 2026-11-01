package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/config"
)

func TestLoadDatabaseDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(env(nil))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.Database.URL != "" {
		t.Errorf("Database.URL = %q, want empty (memory mode)", cfg.Database.URL)
	}
	if cfg.Database.MaxOpenConns != 25 {
		t.Errorf("MaxOpenConns = %d, want 25", cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns != 25 {
		t.Errorf("MaxIdleConns = %d, want 25", cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime != 30*time.Minute {
		t.Errorf("ConnMaxLifetime = %v, want 30m", cfg.Database.ConnMaxLifetime)
	}
	if cfg.Database.ConnMaxIdleTime != 5*time.Minute {
		t.Errorf("ConnMaxIdleTime = %v, want 5m", cfg.Database.ConnMaxIdleTime)
	}
	if !cfg.Database.AutoMigrate {
		t.Error("AutoMigrate = false, want true")
	}
}

func TestLoadDatabaseFromEnv(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"DATABASE_URL":          "postgres://user:pass@localhost:5432/procure?sslmode=disable",
		"DB_MAX_OPEN_CONNS":     "10",
		"DB_MAX_IDLE_CONNS":     "5",
		"DB_CONN_MAX_LIFETIME":  "1h",
		"DB_CONN_MAX_IDLE_TIME": "2m",
		"DB_AUTO_MIGRATE":       "false",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	db := cfg.Database
	if db.URL == "" {
		t.Fatal("Database.URL is empty, want the configured URL")
	}
	if db.MaxOpenConns != 10 || db.MaxIdleConns != 5 {
		t.Errorf("pool = (%d, %d), want (10, 5)", db.MaxOpenConns, db.MaxIdleConns)
	}
	if db.ConnMaxLifetime != time.Hour {
		t.Errorf("ConnMaxLifetime = %v, want 1h", db.ConnMaxLifetime)
	}
	if db.ConnMaxIdleTime != 2*time.Minute {
		t.Errorf("ConnMaxIdleTime = %v, want 2m", db.ConnMaxIdleTime)
	}
	if db.AutoMigrate {
		t.Error("AutoMigrate = true, want false")
	}
}

func TestLoadDatabaseInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "unsupported scheme",
			env:  map[string]string{"DATABASE_URL": "mysql://localhost/procure"},
			want: "DATABASE_URL",
		},
		{
			name: "not a url",
			env:  map[string]string{"DATABASE_URL": "localhost:5432"},
			want: "DATABASE_URL",
		},
		{
			name: "idle exceeds open",
			env: map[string]string{
				"DATABASE_URL":      "postgres://localhost/procure",
				"DB_MAX_OPEN_CONNS": "5",
				"DB_MAX_IDLE_CONNS": "10",
			},
			want: "DB_MAX_IDLE_CONNS",
		},
		{
			name: "invalid pool size",
			env:  map[string]string{"DB_MAX_OPEN_CONNS": "many"},
			want: "DB_MAX_OPEN_CONNS",
		},
		{
			name: "invalid boolean",
			env:  map[string]string{"DB_AUTO_MIGRATE": "maybe"},
			want: "DB_AUTO_MIGRATE",
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

func TestLoadDatabaseRequiredInProduction(t *testing.T) {
	_, err := config.LoadFrom(env(map[string]string{"APP_ENV": "production"}))
	if err == nil {
		t.Fatal("LoadFrom() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error = %q, want it to mention DATABASE_URL", err)
	}

	if _, err := config.LoadFrom(env(map[string]string{
		"APP_ENV":       "production",
		"DATABASE_URL":  "postgres://localhost/procure",
		"AUTH_API_KEYS": testAPIKeys,
	})); err != nil {
		t.Errorf("LoadFrom() with DATABASE_URL error = %v, want nil", err)
	}
}
