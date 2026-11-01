-- 0003_create_idempotency_keys.up.sql
CREATE TABLE idempotency_keys (
    actor_id     text        NOT NULL,
    key          text        NOT NULL,
    request_hash text        NOT NULL,
    status       integer     NOT NULL DEFAULT 0,
    content_type text        NOT NULL DEFAULT '',
    location     text        NOT NULL DEFAULT '',
    body         bytea       NOT NULL DEFAULT ''::bytea,
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    PRIMARY KEY (actor_id, key)
);

-- Supports periodic cleanup of expired claims.
CREATE INDEX idempotency_keys_expires_at_idx ON idempotency_keys (expires_at);
