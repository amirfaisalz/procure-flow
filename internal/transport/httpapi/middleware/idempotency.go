package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

const (
	// IdempotencyHeader carries the client-supplied idempotency key.
	IdempotencyHeader = "Idempotency-Key"
	// IdempotentReplayedHeader marks a response served from the replay store.
	IdempotentReplayedHeader = "Idempotent-Replayed"

	maxIdempotencyKeyLength = 255
)

// Idempotency replays the stored response for a repeated Idempotency-Key and
// rejects reuse of the same key with a different request body. Only wrap write
// endpoints: the middleware buffers the whole response so it can be persisted.
func Idempotency(repo idempotency.Repository, ttl time.Duration, now func() time.Time, logger *slog.Logger) Middleware {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get(IdempotencyHeader))
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !validIdempotencyKey(key) {
				response.WriteProblem(w, r, response.NewProblem(
					http.StatusBadRequest,
					"INVALID_IDEMPOTENCY_KEY",
					"Invalid idempotency key",
					"Idempotency-Key must be 1-255 printable ASCII characters",
				))
				return
			}

			actor, ok := app.ActorFrom(r.Context())
			if !ok {
				// Authentication normally runs first; fail closed if it did not.
				response.WriteProblem(w, r, response.NewProblem(
					http.StatusForbidden,
					"FORBIDDEN",
					"Forbidden",
					"an authenticated actor is required for idempotent requests",
				))
				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, response.DefaultMaxBodyBytes)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				var maxBytesErr *http.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					response.WriteProblem(w, r, response.NewProblem(
						http.StatusRequestEntityTooLarge,
						"PAYLOAD_TOO_LARGE",
						"Request body too large",
						"the request body exceeds the allowed size",
					))
					return
				}
				response.WriteProblem(w, r, response.NewProblem(
					http.StatusBadRequest,
					"INVALID_BODY",
					"Invalid request body",
					"the request body could not be read",
				))
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))

			createdAt := now().UTC()
			record := idempotency.Record{
				ActorID:     actor.Subject,
				Key:         key,
				RequestHash: requestHash(r, body),
				CreatedAt:   createdAt,
				ExpiresAt:   createdAt.Add(ttl),
			}

			existing, claimed, err := repo.Claim(r.Context(), record)
			if err != nil {
				logger.ErrorContext(r.Context(), "idempotency claim failed",
					slog.String("error", err.Error()),
					slog.String("request_id", requestid.From(r.Context())),
				)
				response.WriteProblem(w, r, response.NewProblem(
					http.StatusInternalServerError,
					"INTERNAL_ERROR",
					"Internal server error",
					"an unexpected error occurred",
				))
				return
			}
			if !claimed {
				replay(w, r, existing, record.RequestHash)
				return
			}

			recorder := &bufferedWriter{header: make(http.Header)}
			completed := false
			defer func() {
				if completed {
					return
				}
				// Release the claim so the client can retry after a failure or
				// panic instead of waiting for the TTL to expire.
				if err := repo.Release(context.WithoutCancel(r.Context()), actor.Subject, key); err != nil {
					logger.ErrorContext(r.Context(), "idempotency release failed",
						slog.String("error", err.Error()),
						slog.String("request_id", requestid.From(r.Context())),
					)
				}
			}()

			next.ServeHTTP(recorder, r)

			status := recorder.statusCode()
			if status >= http.StatusInternalServerError {
				writeThrough(w, recorder)
				return
			}

			if err := repo.Complete(r.Context(), actor.Subject, key, status, recorder.header.Get("Content-Type"), recorder.header.Get("Location"), recorder.body.Bytes()); err != nil {
				logger.ErrorContext(r.Context(), "idempotency complete failed",
					slog.String("error", err.Error()),
					slog.String("request_id", requestid.From(r.Context())),
				)
				// Keep the claim. The handler already committed its side effect,
				// so releasing it would let a retry execute the write twice.
				// Retries see IDEMPOTENCY_IN_PROGRESS until the TTL expires.
			}
			completed = true
			writeThrough(w, recorder)
		})
	}
}

func replay(w http.ResponseWriter, r *http.Request, stored idempotency.Record, hash string) {
	if stored.RequestHash != hash {
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusUnprocessableEntity,
			"IDEMPOTENCY_KEY_REUSED",
			"Idempotency key reused",
			"the same Idempotency-Key was used with a different request",
		))
		return
	}
	if stored.InProgress() {
		w.Header().Set("Retry-After", "1")
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusConflict,
			"IDEMPOTENCY_IN_PROGRESS",
			"Request in progress",
			"a request with this Idempotency-Key is still being processed",
		))
		return
	}

	if stored.ContentType != "" {
		w.Header().Set("Content-Type", stored.ContentType)
	}
	if stored.Location != "" {
		w.Header().Set("Location", stored.Location)
	}
	w.Header().Set(IdempotentReplayedHeader, "true")
	w.WriteHeader(stored.Status)
	_, _ = w.Write(stored.Body)
}

func writeThrough(w http.ResponseWriter, recorder *bufferedWriter) {
	for key, values := range recorder.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(recorder.statusCode())
	_, _ = w.Write(recorder.body.Bytes())
}

func requestHash(r *http.Request, body []byte) string {
	sum := sha256.New()
	_, _ = sum.Write([]byte(r.Method))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(r.URL.RequestURI()))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(r.Header.Get("Content-Type")))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write(body)
	return hex.EncodeToString(sum.Sum(nil))
}

func validIdempotencyKey(key string) bool {
	if len(key) == 0 || len(key) > maxIdempotencyKeyLength {
		return false
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

// bufferedWriter captures a response so it can be stored before being sent.
type bufferedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedWriter) Header() http.Header {
	return b.header
}

func (b *bufferedWriter) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferedWriter) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

func (b *bufferedWriter) statusCode() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

// RetryAfterSeconds renders a duration as whole seconds, rounded up.
func RetryAfterSeconds(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	seconds := int64((d + time.Second - 1) / time.Second)
	return strconv.FormatInt(seconds, 10)
}
