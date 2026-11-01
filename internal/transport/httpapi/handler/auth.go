package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	authapp "github.com/amirfaisalz/procure-flow/internal/application/auth"
	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

// sessionCookiePath scopes the session cookie to the API surface only.
const sessionCookiePath = "/api/v1"

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authUserResponse struct {
	ID         string   `json:"id"`
	Email      string   `json:"email"`
	Name       string   `json:"name"`
	Roles      []string `json:"roles"`
	Department string   `json:"department,omitempty"`
}

type loginResponse struct {
	User      authUserResponse `json:"user"`
	ExpiresAt time.Time        `json:"expires_at"`
}

// AuthConfig controls how the session cookie is written.
type AuthConfig struct {
	CookieName   string
	CookieSecure bool
}

// Auth exposes login, logout, and the current-session endpoint.
//
// A nil service disables the session endpoints while keeping /me available for
// API-key callers, which is what happens when AUTH_JWT_SECRET is not set.
type Auth struct {
	service    *authapp.Service
	cookieName string
	secure     bool
	ttl        time.Duration
	logger     *slog.Logger
}

// NewAuth creates the authentication HTTP handler.
func NewAuth(service *authapp.Service, cfg AuthConfig, logger *slog.Logger) *Auth {
	if logger == nil {
		logger = slog.Default()
	}
	name := cfg.CookieName
	if name == "" {
		name = "procure_session"
	}
	ttl := authapp.DefaultSessionTTL
	if service != nil {
		ttl = service.TTL()
	}
	return &Auth{
		service:    service,
		cookieName: name,
		secure:     cfg.CookieSecure,
		ttl:        ttl,
		logger:     logger,
	}
}

// SessionEnabled reports whether login and logout are available. They require a
// signing key, so API-key-only deployments do not expose them.
func (h *Auth) SessionEnabled() bool {
	return h.service != nil
}

// Login handles POST /api/v1/auth/login. On success the signed JWT is returned
// only as an HttpOnly cookie, so scripts cannot read it; the body carries the
// account and the expiry for UI state.
func (h *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := response.DecodeJSON(w, r, &req); err != nil {
		WriteError(w, r, h.logger, err)
		return
	}

	session, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, authapp.ErrInvalidCredentials) {
			h.logger.WarnContext(r.Context(), "login rejected",
				slog.String("request_id", requestid.From(r.Context())),
			)
		}
		WriteError(w, r, h.logger, err)
		return
	}

	h.writeSessionCookie(w, session.Token, session.ExpiresAt)

	h.logger.InfoContext(r.Context(), "login succeeded",
		slog.String("request_id", requestid.From(r.Context())),
		slog.String("subject", session.User.ID),
		slog.Any("roles", session.User.Roles),
	)

	response.JSON(w, http.StatusOK, loginResponse{
		User: authUserResponse{
			ID:         session.User.ID,
			Email:      session.User.Email,
			Name:       session.User.Name,
			Roles:      session.User.Roles,
			Department: session.User.Department,
		},
		ExpiresAt: session.ExpiresAt,
	})
}

// Logout handles POST /api/v1/auth/logout. It clears the cookie even when the
// session has already expired, so clients can always reset their state.
func (h *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	h.clearSessionCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /api/v1/auth/me and reports the authenticated caller from the
// verified token claims.
func (h *Auth) Me(w http.ResponseWriter, r *http.Request) {
	actor, ok := app.ActorFrom(r.Context())
	if !ok {
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusUnauthorized,
			"UNAUTHENTICATED",
			"Authentication required",
			"provide a valid session cookie or bearer token",
		))
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	response.JSON(w, http.StatusOK, authUserResponse{
		ID:         actor.Subject,
		Email:      actor.Email,
		Name:       actor.Name,
		Roles:      actor.Roles,
		Department: actor.Department,
	})
}

func (h *Auth) writeSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName,
		Value:    token,
		Path:     sessionCookiePath,
		Expires:  expiresAt,
		MaxAge:   int(h.ttl.Seconds()),
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
}

func (h *Auth) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName,
		Value:    "",
		Path:     sessionCookiePath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}
