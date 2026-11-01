package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/platform/auth"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/handler"
)

const (
	adminToken     = "test-admin-token-0123456789"
	requesterToken = "test-requester-token-0123456789"
	otherToken     = "test-other-token-0123456789"
	approverToken  = "test-approver-token-0123456789"
	viewerToken    = "test-viewer-token-0123456789"
)

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func buildAuthenticatedRouter(
	t *testing.T,
	repo *memory.ProcurementRepository,
	auditRepo *memory.AuditRepository,
) http.Handler {
	t.Helper()

	service := app.NewService(
		repo,
		&sequentialIDs{},
		func() time.Time { return testNow },
		app.WithAudit(auditRepo, nil),
	)
	health := handler.NewHealth("test", map[string]handler.Check{"repository": repo.Ping})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	authenticator := auth.NewAPIKeyAuthenticator([]auth.APIKey{
		{Token: adminToken, Principal: auth.Principal{Subject: "user_admin", Name: "Admin", Roles: []auth.Role{auth.RoleAdmin}}},
		{Token: requesterToken, Principal: auth.Principal{Subject: "user_amir", Name: "Amir", Roles: []auth.Role{auth.RoleRequester}, Department: "Engineering"}},
		{Token: otherToken, Principal: auth.Principal{Subject: "user_budi", Name: "Budi", Roles: []auth.Role{auth.RoleRequester}, Department: "Engineering"}},
		{Token: approverToken, Principal: auth.Principal{Subject: "user_fin", Name: "Fin", Roles: []auth.Role{auth.RoleApprover}, Department: "Engineering"}},
		{Token: viewerToken, Principal: auth.Principal{Subject: "user_viewer", Name: "Viewer", Roles: []auth.Role{auth.RoleViewer}}},
	})

	return httpapi.NewRouter(httpapi.Dependencies{
		Config: config.Config{
			Env:  config.EnvDevelopment,
			Auth: config.Auth{Enabled: true},
			HTTP: config.HTTP{Addr: ":0", RequestTimeout: 5 * time.Second},
		},
		Logger:        logger,
		Procurement:   service,
		Health:        health,
		Authenticator: authenticator,
	})
}

func TestRouterRejectsAnonymousAPICalls(t *testing.T) {
	repo := memory.NewProcurementRepository()
	router := buildAuthenticatedRouter(t, repo, memory.NewAuditRepository())

	rec := doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements", "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	problem := decodeBody[problemBody](t, rec)
	if problem.Code != "UNAUTHENTICATED" {
		t.Errorf("code = %q, want UNAUTHENTICATED", problem.Code)
	}

	// Health and documentation stay public.
	if rec := doRequestWithHeaders(router, http.MethodGet, "/healthz", "", "", nil); rec.Code != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", rec.Code)
	}
}

func TestRouterEnforcesRBACAndRecordsAudit(t *testing.T) {
	repo := memory.NewProcurementRepository()
	auditRepo := memory.NewAuditRepository()
	router := buildAuthenticatedRouter(t, repo, auditRepo)

	const body = `{"title":"Laptop procurement for engineering","description":"20 units","department":"Engineering","requester":"","amount":250000000,"currency":"IDR"}`

	// A viewer cannot create.
	rec := doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements", body, "application/json", bearer(viewerToken))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}

	// A requester creates and the requester is stamped from the token.
	rec = doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements", body, "application/json", bearer(requesterToken))
	if rec.Code != http.StatusCreated {
		t.Fatalf("requester create status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[envelope](t, rec)
	var procurement procurementBody
	if err := json.Unmarshal(created.Data, &procurement); err != nil {
		t.Fatalf("decode procurement: %v", err)
	}
	if procurement.RequesterID != "user_amir" || procurement.Requester != "Amir" {
		t.Errorf("requester = %q/%q, want user_amir/Amir", procurement.RequesterID, procurement.Requester)
	}

	// Another requester cannot submit it.
	rec = doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements/"+procurement.ID+"/submit", "", "", bearer(otherToken))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other submit status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}

	// The owner submits it.
	rec = doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements/"+procurement.ID+"/submit", "", "", bearer(requesterToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("submit status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// A matching-department approver can approve it.
	rec = doRequestWithHeaders(router, http.MethodPost, "/api/v1/procurements/"+procurement.ID+"/approve", "", "", bearer(approverToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// The audit trail is readable and newest first.
	rec = doRequestWithHeaders(router, http.MethodGet, "/api/v1/procurements/"+procurement.ID+"/audit", "", "", bearer(viewerToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("audit status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var auditEnvelope struct {
		Data []struct {
			Action    string `json:"action"`
			ActorID   string `json:"actor_id"`
			ActorRole string `json:"actor_role"`
			ToStatus  string `json:"to_status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &auditEnvelope); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(auditEnvelope.Data) != 3 {
		t.Fatalf("audit events = %d, want 3", len(auditEnvelope.Data))
	}
	wantActions := []string{app.ActionApproved, app.ActionSubmitted, app.ActionCreated}
	for i, want := range wantActions {
		if auditEnvelope.Data[i].Action != want {
			t.Errorf("events[%d].Action = %q, want %q", i, auditEnvelope.Data[i].Action, want)
		}
	}
	if auditEnvelope.Data[0].ActorID != "user_fin" || auditEnvelope.Data[0].ActorRole != app.RoleApprover {
		t.Errorf("approval actor = %+v, want the approver", auditEnvelope.Data[0])
	}
}
