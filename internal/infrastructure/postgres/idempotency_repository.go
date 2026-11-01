package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
)

const idempotencyColumns = `actor_id, key, request_hash, status, content_type, location, body, created_at, expires_at`

// IdempotencyRepository is the PostgreSQL adapter for the idempotency port.
type IdempotencyRepository struct {
	db *sql.DB
}

// NewIdempotencyRepository creates an idempotency repository backed by db.
func NewIdempotencyRepository(db *sql.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// Claim atomically reserves a key unless an unexpired record already exists.
func (r *IdempotencyRepository) Claim(ctx context.Context, record idempotency.Record) (idempotency.Record, bool, error) {
	q := querierFrom(ctx, r.db)

	result, err := q.ExecContext(ctx, `
INSERT INTO idempotency_keys (`+idempotencyColumns+`)
VALUES ($1, $2, $3, 0, '', '', ''::bytea, $4, $5)
ON CONFLICT (actor_id, key) DO NOTHING`,
		record.ActorID, record.Key, record.RequestHash, record.CreatedAt.UTC(), record.ExpiresAt.UTC(),
	)
	if err != nil {
		return idempotency.Record{}, false, fmt.Errorf("postgres: claim idempotency key: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return idempotency.Record{}, false, fmt.Errorf("postgres: claim idempotency rows affected: %w", err)
	}
	if affected == 1 {
		return record, true, nil
	}

	existing, err := r.get(ctx, record.ActorID, record.Key)
	if err != nil {
		return idempotency.Record{}, false, err
	}
	if existing.ExpiresAt.After(record.CreatedAt) {
		return existing, false, nil
	}

	// The previous claim expired; take it over atomically.
	result, err = q.ExecContext(ctx, `
UPDATE idempotency_keys SET
	request_hash = $3,
	status       = 0,
	content_type = '',
	location     = '',
	body         = ''::bytea,
	created_at   = $4,
	expires_at   = $5
WHERE actor_id = $1 AND key = $2 AND expires_at <= $4`,
		record.ActorID, record.Key, record.RequestHash, record.CreatedAt.UTC(), record.ExpiresAt.UTC(),
	)
	if err != nil {
		return idempotency.Record{}, false, fmt.Errorf("postgres: reclaim idempotency key: %w", err)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return idempotency.Record{}, false, fmt.Errorf("postgres: reclaim idempotency rows affected: %w", err)
	}
	if affected == 1 {
		return record, true, nil
	}

	existing, err = r.get(ctx, record.ActorID, record.Key)
	if err != nil {
		return idempotency.Record{}, false, err
	}
	return existing, false, nil
}

// Complete stores the response for a claimed key.
func (r *IdempotencyRepository) Complete(ctx context.Context, actorID, key string, status int, contentType, location string, body []byte) error {
	result, err := querierFrom(ctx, r.db).ExecContext(ctx, `
UPDATE idempotency_keys SET
	status       = $3,
	content_type = $4,
	location     = $5,
	body         = $6
WHERE actor_id = $1 AND key = $2`,
		actorID, key, status, contentType, location, body,
	)
	if err != nil {
		return fmt.Errorf("postgres: complete idempotency key: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres: complete idempotency rows affected: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("postgres: complete idempotency key %q: record not found", key)
	}
	return nil
}

// Release removes an uncompleted claim so the request can be retried.
func (r *IdempotencyRepository) Release(ctx context.Context, actorID, key string) error {
	if _, err := querierFrom(ctx, r.db).ExecContext(ctx,
		`DELETE FROM idempotency_keys WHERE actor_id = $1 AND key = $2 AND status = 0`,
		actorID, key,
	); err != nil {
		return fmt.Errorf("postgres: release idempotency key: %w", err)
	}
	return nil
}

func (r *IdempotencyRepository) get(ctx context.Context, actorID, key string) (idempotency.Record, error) {
	row := querierFrom(ctx, r.db).QueryRowContext(ctx,
		`SELECT `+idempotencyColumns+` FROM idempotency_keys WHERE actor_id = $1 AND key = $2`,
		actorID, key,
	)

	var record idempotency.Record
	err := row.Scan(
		&record.ActorID, &record.Key, &record.RequestHash, &record.Status, &record.ContentType,
		&record.Location, &record.Body, &record.CreatedAt, &record.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return idempotency.Record{}, fmt.Errorf("postgres: idempotency key %q disappeared during claim", key)
	}
	if err != nil {
		return idempotency.Record{}, fmt.Errorf("postgres: get idempotency key: %w", err)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.ExpiresAt = record.ExpiresAt.UTC()
	return record, nil
}
