package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
)

// Env identifies the deployment environment.
type Env string

const (
	EnvDevelopment Env = "development"
	EnvStaging     Env = "staging"
	EnvProduction  Env = "production"
)

// IsProduction reports whether the application runs in production.
func (e Env) IsProduction() bool {
	return e == EnvProduction
}

// Config is the fully validated application configuration.
type Config struct {
	Env      Env
	LogLevel slog.Level
	HTTP     HTTP
	CORS     CORS
	Docs     Docs
	Database Database
	Auth     Auth
}

// Auth controls request authentication and role-based authorization.
type Auth struct {
	Enabled     bool
	JWTSecret   string
	JWTIssuer   string
	JWTAudience string
	JWTLeeway   time.Duration
	APIKeys     []APIKey
}

// APIKey is a static bearer token bound to a principal.
type APIKey struct {
	Token      string
	Subject    string
	Name       string
	Roles      []auth.Role
	Department string
}

// Database controls the optional PostgreSQL persistence adapter. An empty URL
// selects the in-memory repository (development and tests only).
type Database struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	AutoMigrate     bool
}

// Docs controls the interactive API reference endpoints.
type Docs struct {
	Enabled bool
}

// HTTP contains server and request lifecycle settings.
type HTTP struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	RequestTimeout    time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

// CORS contains browser origin allow-list settings.
type CORS struct {
	AllowedOrigins []string
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	return LoadFrom(os.Getenv)
}

// LoadFrom is the testable form of Load.
func LoadFrom(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	env, err := parseEnv(getenv("APP_ENV"))
	if err != nil {
		return Config{}, err
	}

	level, err := parseLogLevel(getenv("LOG_LEVEL"))
	if err != nil {
		return Config{}, err
	}

	httpConfig, err := loadHTTP(getenv)
	if err != nil {
		return Config{}, err
	}

	corsConfig, err := loadCORS(getenv("CORS_ALLOWED_ORIGINS"))
	if err != nil {
		return Config{}, err
	}

	docsConfig, err := loadDocs(getenv("DOCS_ENABLED"), env)
	if err != nil {
		return Config{}, err
	}

	databaseConfig, err := loadDatabase(getenv)
	if err != nil {
		return Config{}, err
	}

	authConfig, err := loadAuth(getenv, env)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:      env,
		LogLevel: level,
		HTTP:     httpConfig,
		CORS:     corsConfig,
		Docs:     docsConfig,
		Database: databaseConfig,
		Auth:     authConfig,
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.HTTP.RequestTimeout > c.HTTP.WriteTimeout {
		return fmt.Errorf("HTTP_REQUEST_TIMEOUT: must be less than or equal to HTTP_WRITE_TIMEOUT")
	}
	if c.Env.IsProduction() {
		if c.Database.URL == "" {
			return fmt.Errorf("DATABASE_URL: required in production (the in-memory repository is not durable)")
		}
		if !c.Auth.Enabled {
			return fmt.Errorf("AUTH_ENABLED: must be true in production")
		}
	}

	if c.Auth.Enabled {
		if c.Auth.JWTSecret != "" && len(c.Auth.JWTSecret) < 32 {
			return fmt.Errorf("AUTH_JWT_SECRET: must be at least 32 characters")
		}
		if len(c.Auth.APIKeys) == 0 && c.Auth.JWTSecret == "" {
			return fmt.Errorf("AUTH_API_KEYS or AUTH_JWT_SECRET: at least one credential source is required when auth is enabled")
		}
	}
	return nil
}

func parseEnv(raw string) (Env, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return EnvDevelopment, nil
	}

	switch Env(raw) {
	case EnvDevelopment, EnvStaging, EnvProduction:
		return Env(raw), nil
	default:
		return "", fmt.Errorf("APP_ENV: must be one of development, staging, production")
	}
}

func parseLogLevel(raw string) (slog.Level, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return slog.LevelInfo, nil
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		return 0, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	return level, nil
}

func loadHTTP(getenv func(string) string) (HTTP, error) {
	addr := strings.TrimSpace(getenv("HTTP_ADDR"))
	if addr == "" {
		addr = ":8080"
	}

	readHeaderTimeout, err := durationEnv(getenv, "HTTP_READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return HTTP{}, err
	}
	readTimeout, err := durationEnv(getenv, "HTTP_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return HTTP{}, err
	}
	writeTimeout, err := durationEnv(getenv, "HTTP_WRITE_TIMEOUT", 30*time.Second)
	if err != nil {
		return HTTP{}, err
	}
	idleTimeout, err := durationEnv(getenv, "HTTP_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return HTTP{}, err
	}
	requestTimeout, err := durationEnv(getenv, "HTTP_REQUEST_TIMEOUT", 20*time.Second)
	if err != nil {
		return HTTP{}, err
	}
	shutdownTimeout, err := durationEnv(getenv, "HTTP_SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return HTTP{}, err
	}
	maxHeaderBytes, err := intEnv(getenv, "HTTP_MAX_HEADER_BYTES", 1<<20)
	if err != nil {
		return HTTP{}, err
	}

	return HTTP{
		Addr:              addr,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		RequestTimeout:    requestTimeout,
		ShutdownTimeout:   shutdownTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}, nil
}

func loadCORS(raw string) (CORS, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CORS{}, nil
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin == "" {
			return CORS{}, fmt.Errorf("CORS_ALLOWED_ORIGINS: must not contain empty entries")
		}
		if err := validateOrigin(origin); err != nil {
			return CORS{}, fmt.Errorf("CORS_ALLOWED_ORIGINS: %w", err)
		}
		origin = strings.TrimSuffix(origin, "/")
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}

	return CORS{AllowedOrigins: origins}, nil
}

func loadDatabase(getenv func(string) string) (Database, error) {
	db := Database{
		URL:             strings.TrimSpace(getenv("DATABASE_URL")),
		MaxOpenConns:    25,
		MaxIdleConns:    25,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
		AutoMigrate:     true,
	}

	var err error
	if db.MaxOpenConns, err = intEnv(getenv, "DB_MAX_OPEN_CONNS", db.MaxOpenConns); err != nil {
		return Database{}, err
	}
	if db.MaxIdleConns, err = intEnv(getenv, "DB_MAX_IDLE_CONNS", db.MaxIdleConns); err != nil {
		return Database{}, err
	}
	if db.ConnMaxLifetime, err = durationEnv(getenv, "DB_CONN_MAX_LIFETIME", db.ConnMaxLifetime); err != nil {
		return Database{}, err
	}
	if db.ConnMaxIdleTime, err = durationEnv(getenv, "DB_CONN_MAX_IDLE_TIME", db.ConnMaxIdleTime); err != nil {
		return Database{}, err
	}
	if db.AutoMigrate, err = boolEnv(getenv, "DB_AUTO_MIGRATE", db.AutoMigrate); err != nil {
		return Database{}, err
	}

	if db.URL != "" {
		u, err := url.Parse(db.URL)
		if err != nil {
			return Database{}, fmt.Errorf("DATABASE_URL: invalid URL: %w", err)
		}
		if u.Scheme != "postgres" && u.Scheme != "postgresql" {
			return Database{}, fmt.Errorf("DATABASE_URL: scheme must be postgres or postgresql, got %q", u.Scheme)
		}
		if u.Host == "" {
			return Database{}, fmt.Errorf("DATABASE_URL: must include a host")
		}
	}

	if db.MaxIdleConns > db.MaxOpenConns {
		return Database{}, fmt.Errorf("DB_MAX_IDLE_CONNS: must not exceed DB_MAX_OPEN_CONNS")
	}

	return db, nil
}

func loadAuth(getenv func(string) string, env Env) (Auth, error) {
	enabled, err := boolEnv(getenv, "AUTH_ENABLED", env != EnvDevelopment)
	if err != nil {
		return Auth{}, err
	}

	cfg := Auth{
		Enabled:     enabled,
		JWTSecret:   strings.TrimSpace(getenv("AUTH_JWT_SECRET")),
		JWTIssuer:   strings.TrimSpace(getenv("AUTH_JWT_ISSUER")),
		JWTAudience: strings.TrimSpace(getenv("AUTH_JWT_AUDIENCE")),
		JWTLeeway:   30 * time.Second,
	}
	if cfg.JWTIssuer == "" {
		cfg.JWTIssuer = "procure-flow"
	}
	if cfg.JWTAudience == "" {
		cfg.JWTAudience = "procure-flow-api"
	}
	if cfg.JWTLeeway, err = durationEnv(getenv, "AUTH_JWT_LEEWAY", cfg.JWTLeeway); err != nil {
		return Auth{}, err
	}

	cfg.APIKeys, err = parseAPIKeys(getenv("AUTH_API_KEYS"))
	if err != nil {
		return Auth{}, err
	}
	return cfg, nil
}

type apiKeyPayload struct {
	Token      string   `json:"token"`
	Subject    string   `json:"subject"`
	Name       string   `json:"name"`
	Roles      []string `json:"roles"`
	Department string   `json:"department"`
}

func parseAPIKeys(raw string) ([]APIKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()

	var payload []apiKeyPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("AUTH_API_KEYS: invalid JSON: %w", err)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("AUTH_API_KEYS: must contain at least one key")
	}

	keys := make([]APIKey, 0, len(payload))
	seen := make(map[string]struct{}, len(payload))
	for i, item := range payload {
		token := strings.TrimSpace(item.Token)
		if len(token) < 16 {
			return nil, fmt.Errorf("AUTH_API_KEYS[%d]: token must be at least 16 characters", i)
		}
		if _, exists := seen[token]; exists {
			return nil, fmt.Errorf("AUTH_API_KEYS[%d]: duplicate token", i)
		}
		seen[token] = struct{}{}

		subject := strings.TrimSpace(item.Subject)
		if subject == "" {
			return nil, fmt.Errorf("AUTH_API_KEYS[%d]: subject must not be empty", i)
		}

		roles, err := auth.ParseRoles(item.Roles)
		if err != nil {
			return nil, fmt.Errorf("AUTH_API_KEYS[%d]: %w", i, err)
		}

		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = subject
		}

		keys = append(keys, APIKey{
			Token:      token,
			Subject:    subject,
			Name:       name,
			Roles:      roles,
			Department: strings.TrimSpace(item.Department),
		})
	}
	return keys, nil
}

func loadDocs(raw string, env Env) (Docs, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// Interactive docs are useful locally and in staging, but should be an
		// explicit opt-in in production.
		return Docs{Enabled: env != EnvProduction}, nil
	}

	enabled, err := strconv.ParseBool(strings.ToLower(raw))
	if err != nil {
		return Docs{}, fmt.Errorf("DOCS_ENABLED: invalid boolean %q", raw)
	}
	return Docs{Enabled: enabled}, nil
}

func validateOrigin(origin string) error {
	if origin == "*" {
		return fmt.Errorf("wildcard origins are not supported")
	}

	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid origin %q: %w", origin, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("origin %q must use http or https", origin)
	}
	if u.Host == "" {
		return fmt.Errorf("origin %q must include a host", origin)
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("origin %q must not include a path", origin)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("origin %q must not include credentials, query, or fragment", origin)
	}
	return nil
}

func durationEnv(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", key, raw, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be greater than zero", key)
	}
	return value, nil
}

func intEnv(getenv func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", key, raw, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be greater than zero", key)
	}
	return value, nil
}

func boolEnv(getenv func(string) string, key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.ParseBool(strings.ToLower(raw))
	if err != nil {
		return false, fmt.Errorf("%s: invalid boolean %q", key, raw)
	}
	return value, nil
}
