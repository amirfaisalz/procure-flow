package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"regexp"
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
	Env          Env
	LogLevel     slog.Level
	HTTP         HTTP
	CORS         CORS
	Docs         Docs
	Database     Database
	Auth         Auth
	Idempotency  Idempotency
	RateLimit    RateLimit
	Metrics      Metrics
	Redis        Redis
	RabbitMQ     RabbitMQ
	Integrations Integrations
}

// Redis controls the optional Redis client used for distributed rate limiting
// and read-through caching. An empty URL disables both.
type Redis struct {
	URL            string
	CacheEnabled   bool
	CacheTTL       time.Duration
	DialTimeout    time.Duration
	CommandTimeout time.Duration
}

// RabbitMQ controls asynchronous event publishing. An empty URL disables the
// outbox relay, and the worker has nothing to consume.
type RabbitMQ struct {
	URL      string
	Exchange string
}

// Integrations configures the optional outbound webhooks used by the worker.
type Integrations struct {
	NotificationWebhookURL string
	ERPWebhookURL          string
}

// Idempotency controls replay protection for write endpoints.
type Idempotency struct {
	Enabled bool
	TTL     time.Duration
}

// RateLimit controls the per-caller token bucket.
type RateLimit struct {
	Enabled           bool
	RequestsPerSecond float64
	Burst             int
	// Store is "auto", "memory", or "redis". Auto uses Redis when REDIS_URL is
	// set and falls back to the in-process limiter otherwise.
	Store string
}

// Rate limit store choices.
const (
	RateLimitStoreAuto   = "auto"
	RateLimitStoreMemory = "memory"
	RateLimitStoreRedis  = "redis"
)

// Metrics controls the Prometheus exposition endpoint.
type Metrics struct {
	Enabled bool
}

// Auth controls request authentication and role-based authorization.
type Auth struct {
	Enabled      bool
	JWTSecret    string
	JWTIssuer    string
	JWTAudience  string
	JWTLeeway    time.Duration
	SessionTTL   time.Duration
	CookieName   string
	CookieSecure bool
	// JWTSecretGenerated reports that a throwaway development signing key was
	// created because AUTH_JWT_SECRET was unset. Sessions do not survive a
	// restart in that mode.
	JWTSecretGenerated bool
	APIKeys            []APIKey
}

// APIKey is a static bearer token bound to a principal.
type APIKey struct {
	Token      string
	Subject    string
	Name       string
	Roles      []auth.Role
	Department string
}

// Database controls the PostgreSQL persistence adapter. The URL is required:
// PostgreSQL is the only storage backend.
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
	cfg, err := loadFrom(getenv)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.validateServer(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadWorker reads configuration for the asynchronous worker. Server-only
// requirements (a database and authentication) are not enforced, but a broker
// URL is mandatory.
func LoadWorker() (Config, error) {
	return LoadWorkerFrom(os.Getenv)
}

// LoadWorkerFrom is the testable form of LoadWorker.
func LoadWorkerFrom(getenv func(string) string) (Config, error) {
	cfg, err := loadFrom(getenv)
	if err != nil {
		return Config{}, err
	}
	if cfg.RabbitMQ.URL == "" {
		return Config{}, fmt.Errorf("RABBITMQ_URL: required for the worker")
	}
	return cfg, nil
}

// LoadDatabase reads only the database settings. Migration tooling needs a
// DATABASE_URL but not HTTP or authentication configuration, so it must not be
// subject to the server's production requirements.
func LoadDatabase() (Database, error) {
	return LoadDatabaseFrom(os.Getenv)
}

// LoadDatabaseFrom is the testable form of LoadDatabase.
func LoadDatabaseFrom(getenv func(string) string) (Database, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	return loadDatabase(getenv)
}

// loadFrom parses every setting without applying deployment-specific policy.
func loadFrom(getenv func(string) string) (Config, error) {
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

	idempotencyConfig, err := loadIdempotency(getenv)
	if err != nil {
		return Config{}, err
	}

	rateLimitConfig, err := loadRateLimit(getenv, env)
	if err != nil {
		return Config{}, err
	}

	metricsConfig, err := loadMetrics(getenv)
	if err != nil {
		return Config{}, err
	}

	redisConfig, err := loadRedis(getenv)
	if err != nil {
		return Config{}, err
	}

	rabbitConfig, err := loadRabbitMQ(getenv)
	if err != nil {
		return Config{}, err
	}

	integrationsConfig, err := loadIntegrations(getenv)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:          env,
		LogLevel:     level,
		HTTP:         httpConfig,
		CORS:         corsConfig,
		Docs:         docsConfig,
		Database:     databaseConfig,
		Auth:         authConfig,
		Idempotency:  idempotencyConfig,
		RateLimit:    rateLimitConfig,
		Metrics:      metricsConfig,
		Redis:        redisConfig,
		RabbitMQ:     rabbitConfig,
		Integrations: integrationsConfig,
	}
	return cfg, nil
}

// validCookieName keeps the session cookie name safe to place in a header.
var validCookieName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (c Config) validateServer() error {
	if c.HTTP.RequestTimeout > c.HTTP.WriteTimeout {
		return fmt.Errorf("HTTP_REQUEST_TIMEOUT: must be less than or equal to HTTP_WRITE_TIMEOUT")
	}
	if c.RateLimit.Enabled && c.RateLimit.Store == RateLimitStoreRedis && c.Redis.URL == "" {
		return fmt.Errorf("RATE_LIMIT_STORE: redis requires REDIS_URL")
	}
	if c.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL: required (PostgreSQL is the only storage backend)")
	}
	if c.Env != EnvDevelopment && !c.Auth.Enabled {
		return fmt.Errorf("AUTH_ENABLED: must be true outside development")
	}

	if c.Auth.Enabled {
		if c.Auth.JWTSecret != "" && len(c.Auth.JWTSecret) < 32 {
			return fmt.Errorf("AUTH_JWT_SECRET: must be at least 32 characters")
		}
		if len(c.Auth.APIKeys) == 0 && c.Auth.JWTSecret == "" {
			return fmt.Errorf("AUTH_API_KEYS or AUTH_JWT_SECRET: at least one credential source is required when auth is enabled")
		}
		if c.Auth.SessionTTL < time.Minute || c.Auth.SessionTTL > 30*24*time.Hour {
			return fmt.Errorf("AUTH_SESSION_TTL: must be between 1m and 720h")
		}
		if !validCookieName.MatchString(c.Auth.CookieName) {
			return fmt.Errorf("AUTH_COOKIE_NAME: must contain only letters, digits, dashes, and underscores")
		}
	}
	return nil
}

func loadIdempotency(getenv func(string) string) (Idempotency, error) {
	enabled, err := boolEnv(getenv, "IDEMPOTENCY_ENABLED", true)
	if err != nil {
		return Idempotency{}, err
	}
	ttl, err := durationEnv(getenv, "IDEMPOTENCY_TTL", 24*time.Hour)
	if err != nil {
		return Idempotency{}, err
	}
	return Idempotency{Enabled: enabled, TTL: ttl}, nil
}

func loadRateLimit(getenv func(string) string, env Env) (RateLimit, error) {
	enabled, err := boolEnv(getenv, "RATE_LIMIT_ENABLED", env != EnvDevelopment)
	if err != nil {
		return RateLimit{}, err
	}
	requestsPerSecond, err := floatEnv(getenv, "RATE_LIMIT_RPS", 20)
	if err != nil {
		return RateLimit{}, err
	}
	burst, err := intEnv(getenv, "RATE_LIMIT_BURST", 40)
	if err != nil {
		return RateLimit{}, err
	}
	store, err := parseRateLimitStore(getenv("RATE_LIMIT_STORE"))
	if err != nil {
		return RateLimit{}, err
	}
	return RateLimit{
		Enabled:           enabled,
		RequestsPerSecond: requestsPerSecond,
		Burst:             burst,
		Store:             store,
	}, nil
}

func parseRateLimitStore(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return RateLimitStoreAuto, nil
	}
	switch raw {
	case RateLimitStoreAuto, RateLimitStoreMemory, RateLimitStoreRedis:
		return raw, nil
	default:
		return "", fmt.Errorf("RATE_LIMIT_STORE: must be auto, memory, or redis")
	}
}

func loadRedis(getenv func(string) string) (Redis, error) {
	cfg := Redis{
		URL:            strings.TrimSpace(getenv("REDIS_URL")),
		CacheTTL:       30 * time.Second,
		DialTimeout:    2 * time.Second,
		CommandTimeout: time.Second,
	}

	cacheEnabled, err := boolEnv(getenv, "CACHE_ENABLED", cfg.URL != "")
	if err != nil {
		return Redis{}, err
	}
	if cacheEnabled && cfg.URL == "" {
		return Redis{}, fmt.Errorf("CACHE_ENABLED: requires REDIS_URL")
	}
	cfg.CacheEnabled = cacheEnabled

	if cfg.CacheTTL, err = durationEnv(getenv, "CACHE_TTL", cfg.CacheTTL); err != nil {
		return Redis{}, err
	}
	if cfg.DialTimeout, err = durationEnv(getenv, "REDIS_DIAL_TIMEOUT", cfg.DialTimeout); err != nil {
		return Redis{}, err
	}
	if cfg.CommandTimeout, err = durationEnv(getenv, "REDIS_COMMAND_TIMEOUT", cfg.CommandTimeout); err != nil {
		return Redis{}, err
	}

	if cfg.URL != "" {
		u, err := url.Parse(cfg.URL)
		if err != nil {
			return Redis{}, fmt.Errorf("REDIS_URL: invalid URL: %w", err)
		}
		if u.Scheme != "redis" && u.Scheme != "rediss" {
			return Redis{}, fmt.Errorf("REDIS_URL: scheme must be redis or rediss, got %q", u.Scheme)
		}
		if u.Host == "" {
			return Redis{}, fmt.Errorf("REDIS_URL: must include a host")
		}
	}

	return cfg, nil
}

func loadRabbitMQ(getenv func(string) string) (RabbitMQ, error) {
	cfg := RabbitMQ{
		URL:      strings.TrimSpace(getenv("RABBITMQ_URL")),
		Exchange: strings.TrimSpace(getenv("RABBITMQ_EXCHANGE")),
	}
	if cfg.Exchange == "" {
		cfg.Exchange = "procure.events"
	}

	if cfg.URL != "" {
		u, err := url.Parse(cfg.URL)
		if err != nil {
			return RabbitMQ{}, fmt.Errorf("RABBITMQ_URL: invalid URL: %w", err)
		}
		if u.Scheme != "amqp" && u.Scheme != "amqps" {
			return RabbitMQ{}, fmt.Errorf("RABBITMQ_URL: scheme must be amqp or amqps, got %q", u.Scheme)
		}
		if u.Host == "" {
			return RabbitMQ{}, fmt.Errorf("RABBITMQ_URL: must include a host")
		}
	}

	return cfg, nil
}

func loadIntegrations(getenv func(string) string) (Integrations, error) {
	cfg := Integrations{
		NotificationWebhookURL: strings.TrimSpace(getenv("NOTIFICATION_WEBHOOK_URL")),
		ERPWebhookURL:          strings.TrimSpace(getenv("ERP_WEBHOOK_URL")),
	}

	for _, item := range []struct {
		key   string
		value string
	}{
		{"NOTIFICATION_WEBHOOK_URL", cfg.NotificationWebhookURL},
		{"ERP_WEBHOOK_URL", cfg.ERPWebhookURL},
	} {
		if item.value == "" {
			continue
		}
		if err := validateWebhookURL(item.value); err != nil {
			return Integrations{}, fmt.Errorf("%s: %w", item.key, err)
		}
	}
	return cfg, nil
}

func validateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must use http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("must include a host")
	}
	return nil
}

func loadMetrics(getenv func(string) string) (Metrics, error) {
	enabled, err := boolEnv(getenv, "METRICS_ENABLED", true)
	if err != nil {
		return Metrics{}, err
	}
	return Metrics{Enabled: enabled}, nil
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
	// Authentication is on by default everywhere; only development may opt out
	// explicitly, and the router then treats every caller as a local admin.
	enabled, err := boolEnv(getenv, "AUTH_ENABLED", true)
	if err != nil {
		return Auth{}, err
	}

	cfg := Auth{
		Enabled:      enabled,
		JWTSecret:    strings.TrimSpace(getenv("AUTH_JWT_SECRET")),
		JWTIssuer:    strings.TrimSpace(getenv("AUTH_JWT_ISSUER")),
		JWTAudience:  strings.TrimSpace(getenv("AUTH_JWT_AUDIENCE")),
		JWTLeeway:    30 * time.Second,
		SessionTTL:   8 * time.Hour,
		CookieName:   "procure_session",
		CookieSecure: env != EnvDevelopment,
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
	if cfg.SessionTTL, err = durationEnv(getenv, "AUTH_SESSION_TTL", cfg.SessionTTL); err != nil {
		return Auth{}, err
	}
	if name := strings.TrimSpace(getenv("AUTH_COOKIE_NAME")); name != "" {
		cfg.CookieName = name
	}
	if cfg.CookieSecure, err = boolEnv(getenv, "AUTH_COOKIE_SECURE", cfg.CookieSecure); err != nil {
		return Auth{}, err
	}

	cfg.APIKeys, err = parseAPIKeys(getenv("AUTH_API_KEYS"))
	if err != nil {
		return Auth{}, err
	}

	// A fresh checkout must be able to log in without configuration. The secret
	// is ephemeral, so development sessions simply expire on restart.
	if cfg.Enabled && env == EnvDevelopment && cfg.JWTSecret == "" {
		secret, err := developmentSecret()
		if err != nil {
			return Auth{}, err
		}
		cfg.JWTSecret = secret
		cfg.JWTSecretGenerated = true
	}
	return cfg, nil
}

// developmentSecret returns a random HS256 key for local runs.
func developmentSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("AUTH_JWT_SECRET: generate development secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
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

func floatEnv(getenv func(string) string, key string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid number %q: %w", key, raw, err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s: must be a finite number", key)
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
