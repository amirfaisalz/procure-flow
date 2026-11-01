package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/middleware"
)

const (
	idempotentBody = `{"title":"Laptop procurement"}`
	otherBody      = `{"title":"Server procurement"}`
)

var idempotentClock = time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC)

func idempotentHandler(calls *int32, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Location", "/api/v1/procurements/prc_1")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"data":{"id":"prc_1"}}`))
	})
}

func wrapIdempotent(repo idempotency.Repository, next http.Handler) http.Handler {
	return middleware.Idempotency(repo, time.Hour, func() time.Time { return idempotentClock }, nil)(next)
}

func serveIdempotent(t *testing.T, h http.Handler, body, key, actor string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/procurements", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req = req.WithContext(app.WithActor(req.Context(), app.Actor{
		Subject: actor, Name: actor, Roles: []string{app.RoleRequester},
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIdempotencyPassesThroughWithoutKey(t *testing.T) {
	var calls int32
	h := wrapIdempotent(testsupport.NewIdempotencyRepository(), idempotentHandler(&calls, http.StatusCreated))

	for i := 0; i < 2; i++ {
		if rec := serveIdempotent(t, h, idempotentBody, "", "user_amir"); rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
	}
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyReplaysStoredResponse(t *testing.T) {
	var calls int32
	h := wrapIdempotent(testsupport.NewIdempotencyRepository(), idempotentHandler(&calls, http.StatusCreated))

	first := serveIdempotent(t, h, idempotentBody, "key-1", "user_amir")
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", first.Code)
	}
	if first.Header().Get(middleware.IdempotentReplayedHeader) != "" {
		t.Error("first response was marked as replayed")
	}

	second := serveIdempotent(t, h, idempotentBody, "key-1", "user_amir")
	if second.Code != http.StatusCreated {
		t.Fatalf("second status = %d, want 201", second.Code)
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
	if second.Header().Get(middleware.IdempotentReplayedHeader) != "true" {
		t.Error("replay header is missing")
	}
	if second.Header().Get("Location") != "/api/v1/procurements/prc_1" {
		t.Errorf("Location = %q", second.Header().Get("Location"))
	}
	if second.Body.String() != first.Body.String() {
		t.Errorf("body = %q, want %q", second.Body.String(), first.Body.String())
	}
}

func TestIdempotencyRejectsReusedKeyWithDifferentBody(t *testing.T) {
	var calls int32
	h := wrapIdempotent(testsupport.NewIdempotencyRepository(), idempotentHandler(&calls, http.StatusCreated))

	if rec := serveIdempotent(t, h, idempotentBody, "key-1", "user_amir"); rec.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", rec.Code)
	}

	rec := serveIdempotent(t, h, otherBody, "key-1", "user_amir")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "IDEMPOTENCY_KEY_REUSED" {
		t.Errorf("code = %q, want IDEMPOTENCY_KEY_REUSED", problem.Code)
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
}

func TestIdempotencyReportsInProgress(t *testing.T) {
	repo := testsupport.NewIdempotencyRepository()
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls int32

	h := middleware.Idempotency(repo, time.Hour, func() time.Time { return idempotentClock }, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&calls, 1) == 1 {
				close(entered)
				<-release
			}
			w.WriteHeader(http.StatusCreated)
		}),
	)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serveIdempotent(t, h, idempotentBody, "key-1", "user_amir")
	}()

	<-entered
	rec := serveIdempotent(t, h, idempotentBody, "key-1", "user_amir")
	close(release)
	<-done

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header is missing")
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "IDEMPOTENCY_IN_PROGRESS" {
		t.Errorf("code = %q, want IDEMPOTENCY_IN_PROGRESS", problem.Code)
	}
}

func TestIdempotencyReleasesClaimOnServerError(t *testing.T) {
	var calls int32
	h := wrapIdempotent(testsupport.NewIdempotencyRepository(), idempotentHandler(&calls, http.StatusInternalServerError))

	if rec := serveIdempotent(t, h, idempotentBody, "key-1", "user_amir"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	// The failed claim was released, so the retry executes again.
	if rec := serveIdempotent(t, h, idempotentBody, "key-1", "user_amir"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("retry status = %d, want 500", rec.Code)
	}
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyKeepsClaimWhenCompletionFails(t *testing.T) {
	var calls int32
	repo := completeFailingRepository{Repository: testsupport.NewIdempotencyRepository()}
	h := middleware.Idempotency(repo, time.Hour, func() time.Time { return idempotentClock }, nil)(
		idempotentHandler(&calls, http.StatusCreated),
	)

	first := serveIdempotent(t, h, idempotentBody, "key-1", "user_amir")
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", first.Code)
	}

	// The write succeeded but storing the replay failed. The claim must stay
	// held so a retry cannot execute the side effect a second time.
	second := serveIdempotent(t, h, idempotentBody, "key-1", "user_amir")
	if second.Code != http.StatusConflict {
		t.Fatalf("retry status = %d, want 409; body = %s", second.Code, second.Body.String())
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
}

type completeFailingRepository struct {
	idempotency.Repository
}

func (completeFailingRepository) Complete(context.Context, string, string, int, string, string, []byte) error {
	return errors.New("storage unavailable")
}

func TestIdempotencyIsScopedPerActor(t *testing.T) {
	var calls int32
	h := wrapIdempotent(testsupport.NewIdempotencyRepository(), idempotentHandler(&calls, http.StatusCreated))

	serveIdempotent(t, h, idempotentBody, "shared-key", "user_amir")
	serveIdempotent(t, h, idempotentBody, "shared-key", "user_budi")
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2 (keys are scoped per actor)", calls)
	}
}

func TestIdempotencyRejectsInvalidKey(t *testing.T) {
	var calls int32
	h := wrapIdempotent(testsupport.NewIdempotencyRepository(), idempotentHandler(&calls, http.StatusCreated))

	rec := serveIdempotent(t, h, idempotentBody, strings.Repeat("k", 256), "user_amir")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if calls != 0 {
		t.Errorf("handler calls = %d, want 0", calls)
	}
}

func TestIdempotencyRejectsOversizedBody(t *testing.T) {
	var calls int32
	h := wrapIdempotent(testsupport.NewIdempotencyRepository(), idempotentHandler(&calls, http.StatusCreated))

	body := strings.Repeat("a", (1<<20)+1)
	rec := serveIdempotent(t, h, body, "key-1", "user_amir")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if calls != 0 {
		t.Errorf("handler calls = %d, want 0", calls)
	}
}

func TestIdempotencyFailsClosedWithoutActor(t *testing.T) {
	var calls int32
	h := wrapIdempotent(testsupport.NewIdempotencyRepository(), idempotentHandler(&calls, http.StatusCreated))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/procurements", strings.NewReader(idempotentBody))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if calls != 0 {
		t.Errorf("handler calls = %d, want 0", calls)
	}
}
