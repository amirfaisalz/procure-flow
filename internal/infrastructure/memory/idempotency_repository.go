package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
)

// IdempotencyRepository is a thread-safe, in-memory idempotency store used for
// local development and tests.
type IdempotencyRepository struct {
	mu      sync.Mutex
	records map[string]idempotency.Record
}

// NewIdempotencyRepository creates an empty idempotency repository.
func NewIdempotencyRepository() *IdempotencyRepository {
	return &IdempotencyRepository{records: make(map[string]idempotency.Record)}
}

// Claim atomically reserves a key unless an unexpired record already exists.
func (r *IdempotencyRepository) Claim(ctx context.Context, record idempotency.Record) (idempotency.Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Record{}, false, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := idempotencyKey(record.ActorID, record.Key)
	if existing, ok := r.records[key]; ok && existing.ExpiresAt.After(record.CreatedAt) {
		return cloneRecord(existing), false, nil
	}

	r.records[key] = cloneRecord(record)
	return cloneRecord(record), true, nil
}

// Complete stores the response for a claimed key.
func (r *IdempotencyRepository) Complete(ctx context.Context, actorID, key string, status int, contentType, location string, body []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	storageKey := idempotencyKey(actorID, key)
	record, ok := r.records[storageKey]
	if !ok {
		return fmt.Errorf("memory: idempotency key %q not found", key)
	}
	record.Status = status
	record.ContentType = contentType
	record.Location = location
	record.Body = append([]byte(nil), body...)
	r.records[storageKey] = record
	return nil
}

// Release removes an uncompleted claim so the request can be retried.
func (r *IdempotencyRepository) Release(ctx context.Context, actorID, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	storageKey := idempotencyKey(actorID, key)
	if record, ok := r.records[storageKey]; ok && record.InProgress() {
		delete(r.records, storageKey)
	}
	return nil
}

func idempotencyKey(actorID, key string) string {
	return actorID + "\x00" + key
}

func cloneRecord(record idempotency.Record) idempotency.Record {
	record.Body = append([]byte(nil), record.Body...)
	return record
}
