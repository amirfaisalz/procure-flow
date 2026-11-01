package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	authapp "github.com/amirfaisalz/procure-flow/internal/application/auth"
	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/domain/user"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
	"github.com/amirfaisalz/procure-flow/internal/platform/metrics"
	"github.com/amirfaisalz/procure-flow/internal/platform/password"
	"github.com/amirfaisalz/procure-flow/internal/platform/ratelimit"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
)

const (
	sessionSecret = "session-secret-0123456789abcdef"
	sessionCookie = "procure_session"
	sessionEmail  = "admin@procure.local"
)

type sessionUserBody struct {
	ID         string   `json:"id"`
	Email      string   `json:"email"`
	Name       string   `json:"name"`
	Roles      []string `json:"roles"`
	Department string   `json:"department"`
}

type sessionBody struct {
	User      sessionUserBody `json:"user"`
	ExpiresAt time.Time       `json:"expires_at"`
}

func buildSessionRouter(t *testing.T, configure func(*httpapi.Dependencies)) http.Handler {
	t.Helper()

	users := memory.NewUserRepository()
	hasher := password.New(bcrypt.MinCost)
	for _, account := range authapp.DemoAccounts() {
		hash, err := hasher.Hash(authapp.DemoPassword)
		if err != nil {
			t.Fatalf("hash %s: %v", account.Email, err)
		}
		record := user.User{
			ID:           "usr_" + account.Roles[0],
			Email:        account.Email,
			Name:         account.Name,
			PasswordHash: hash,
			Roles:        account.Roles,
			Department:   account.Department,
			Active:       true,
			CreatedAt:    testNow,
			UpdatedAt:    testNow,
		}
		if err := users.Upsert(t.Context(), record); err != nil {
			t.Fatalf("seed %s: %v", account.Email, err)
		}
	}

	issuer := auth.NewJWTIssuer([]byte(sessionSecret), "procure-flow", "procure-flow-api")
	loginService := authapp.NewService(users, hasher, issuer, time.Hour)
	authHandler := handler.NewAuth(loginService, handler.AuthConfig{
		CookieName:   sessionCookie,
		CookieSecure: true,
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	procurementRepo := memory.NewProcurementRepository()
	service := app.NewService(procurementRepo, &sequentialIDs{}, func() time.Time { return testNow })
	health := handler.NewHealthWithOptional("test", map[string]handler.Check{"repository": procurementRepo.Ping}, nil)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	authenticator := auth.NewJWTAuthenticator(auth.JWTConfig{
		Secret:     []byte(sessionSecret),
		Issuer:     "procure-flow",
		Audience:   "procure-flow-api",
		CookieName: sessionCookie,
	})

	deps := httpapi.Dependencies{
		Config: config.Config{
			Env:  config.EnvDevelopment,
			Auth: config.Auth{Enabled: true, JWTSecret: sessionSecret},
			HTTP: config.HTTP{Addr: ":0", RequestTimeout: 5 * time.Second},
			Idempotency: config.Idempotency{
				Enabled: true,
				TTL:     time.Hour,
			},
			Metrics: config.Metrics{Enabled: true},
		},
		Logger:        logger,
		Procurement:   service,
		Health:        health,
		Authenticator: authenticator,
		Auth:          authHandler,
		Idempotency:   memory.NewIdempotencyRepository(),
		Metrics:       metrics.NewRegistry(),
	}
	if configure != nil {
		configure(&deps)
	}
	return httpapi.NewRouter(deps)
}

func login(t *testing.T, router http.Handler, email, plain string) *http.Response {
	t.Helper()

	body := `{"email":` + quote(email) + `,"password":` + quote(plain) + `}`
	rec := doRequestWithHeaders(router, http.MethodPost, "/api/v1/auth/login", body, "application/json", nil)
	return rec.Result()
}

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestLoginSetsSecureHttpOnlyCookie(t *testing.T) {
	router := buildSessionRouter(t, nil)

	resp := login(t, router, sessionEmail, authapp.DemoPassword)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookie {
		t.Errorf("cookie name = %q, want %q", cookie.Name, sessionCookie)
	}
	if cookie.Value == "" {
		t.Error("cookie value is empty")
	}
	if !cookie.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if !cookie.Secure {
		t.Error("cookie is not Secure")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/api/v1" {
		t.Errorf("Path = %q, want /api/v1", cookie.Path)
	}
	if cookie.MaxAge != 3600 {
		t.Errorf("MaxAge = %d, want 3600", cookie.MaxAge)
	}

	var body struct {
		Data sessionBody `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data.User.Email != sessionEmail {
		t.Errorf("user email = %q, want %q", body.Data.User.Email, sessionEmail)
	}
	if len(body.Data.User.Roles) != 1 || body.Data.User.Roles[0] != "admin" {
		t.Errorf("roles = %v, want [admin]", body.Data.User.Roles)
	}
}

func TestSessionCookieAuthenticates(t *testing.T) {
	router := buildSessionRouter(t, nil)

	loginResp := login(t, router, sessionEmail, authapp.DemoPassword)
	loginResp.Body.Close()
	cookie := loginResp.Cookies()[0]

	headers := map[string]string{"Cookie": cookie.String()}

	rec := doRequestWithHeaders(router, http.MethodGet, "/api/v1/auth/me", "", "", headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Data sessionUserBody `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.Data.Email != sessionEmail || me.Data.ID != "usr_admin" {
		t.Errorf("me = %+v, want the admin account", me.Data)
	}

	list := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", headers)
	if list.Code != http.StatusOK {
		t.Errorf("procurements status = %d, want 200; body = %s", list.Code, list.Body.String())
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	router := buildSessionRouter(t, nil)

	resp := login(t, router, sessionEmail, "definitely-wrong")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if len(resp.Cookies()) != 0 {
		t.Errorf("got %d cookies, want none", len(resp.Cookies()))
	}

	var problem problemBody
	if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "INVALID_CREDENTIALS" {
		t.Errorf("code = %q, want INVALID_CREDENTIALS", problem.Code)
	}
}

func TestLoginRateLimitedPerIP(t *testing.T) {
	router := buildSessionRouter(t, func(deps *httpapi.Dependencies) {
		deps.Config.RateLimit = config.RateLimit{Enabled: true, RequestsPerSecond: 0.001, Burst: 1}
		deps.RateLimiter = ratelimit.New(0.001, 1)
	})

	first := login(t, router, sessionEmail, authapp.DemoPassword)
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first login status = %d, want 200", first.StatusCode)
	}

	second := login(t, router, sessionEmail, authapp.DemoPassword)
	defer func() { _ = second.Body.Close() }()
	if second.StatusCode != http.StatusTooManyRequests {
		t.Errorf("second login status = %d, want 429", second.StatusCode)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	router := buildSessionRouter(t, nil)

	rec := doRequestWithHeaders(router, http.MethodPost, "/api/v1/auth/logout", "", "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", rec.Code)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if cookies[0].MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want a negative value to delete the cookie", cookies[0].MaxAge)
	}
	if !cookies[0].HttpOnly {
		t.Error("cleared cookie lost HttpOnly")
	}
}

func TestMeRequiresAuthentication(t *testing.T) {
	router := buildSessionRouter(t, nil)

	rec := doRequestWithHeaders(router, http.MethodGet, "/api/v1/auth/me", "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestLoginRejectsMalformedBody(t *testing.T) {
	router := buildSessionRouter(t, nil)

	rec := doRequestWithHeaders(router, http.MethodPost, "/api/v1/auth/login", `{"email":`, "application/json", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}
