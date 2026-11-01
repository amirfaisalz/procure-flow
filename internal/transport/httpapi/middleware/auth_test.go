package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/middleware"
)

type stubAuthenticator struct {
	principal auth.Principal
	err       error
}

func (s stubAuthenticator) Authenticate(context.Context, *http.Request) (auth.Principal, error) {
	return s.principal, s.err
}

func TestAuthenticationInjectsActor(t *testing.T) {
	handler := middleware.Authentication(stubAuthenticator{principal: auth.Principal{
		Subject:    "user_amir",
		Name:       "Amir",
		Roles:      []auth.Role{auth.RoleRequester},
		Department: "Engineering",
	}}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := app.ActorFrom(r.Context())
		if !ok {
			t.Fatal("actor is missing from the request context")
		}
		if actor.Subject != "user_amir" || !actor.HasRole(app.RoleRequester) || actor.Department != "Engineering" {
			t.Errorf("actor = %+v, want the mapped principal", actor)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := serve(handler, http.MethodGet, "/api/v1/procurements", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestAuthenticationRejectsAnonymous(t *testing.T) {
	handler := middleware.Authentication(stubAuthenticator{err: auth.ErrUnauthenticated}, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("downstream handler was called for an unauthenticated request")
		}),
	)

	rec := serve(handler, http.MethodGet, "/api/v1/procurements", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("WWW-Authenticate header is missing")
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want a problem document", got)
	}

	var problem struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Status != http.StatusUnauthorized || problem.Code != "UNAUTHENTICATED" {
		t.Errorf("problem = %+v, want 401 UNAUTHENTICATED", problem)
	}
}

func TestInjectActor(t *testing.T) {
	handler := middleware.InjectActor(app.Actor{
		Subject: "local_admin",
		Name:    "Local Admin",
		Roles:   []string{app.RoleAdmin},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := app.ActorFrom(r.Context())
		if !ok || !actor.IsAdmin() {
			t.Errorf("actor = %+v, want an injected admin", actor)
		}
	}))

	rec := serve(handler, http.MethodGet, "/api/v1/procurements", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
