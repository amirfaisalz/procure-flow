-- 0004_create_outbox_events.up.sql
CREATE TABLE outbox_events (
    id              text        PRIMARY KEY,
    event_type      text        NOT NULL,
    aggregate_type  text        NOT NULL,
    aggregate_id    text        NOT NULL,
    actor_id        text        NOT NULL DEFAULT '',
    payload         jsonb       NOT NULL,
    occurred_at     timestamptz NOT NULL,
    next_attempt_at timestamptz NOT NULL,
    attempts        integer     NOT NULL DEFAULT 0,
    last_error      text        NOT NULL DEFAULT '',
    published_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- The relay polls only unpublished rows whose backoff has elapsed.
CREATE INDEX outbox_events_pending_idx
    ON outbox_events (next_attempt_at)
    WHERE published_at IS NULL;

-- Supports retention pruning without scanning the whole table.
CREATE INDEX outbox_events_published_idx
    ON outbox_events (published_at)
    WHERE published_at IS NOT NULL;
