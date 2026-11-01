-- 0002_create_audit_events.up.sql
CREATE TABLE audit_events (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entity_type text        NOT NULL,
    entity_id   text        NOT NULL,
    action      text        NOT NULL,
    actor_id    text        NOT NULL,
    actor_name  text        NOT NULL DEFAULT '',
    actor_role  text        NOT NULL DEFAULT '',
    from_status text        NOT NULL DEFAULT '',
    to_status   text        NOT NULL DEFAULT '',
    metadata    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL
);

CREATE INDEX audit_events_entity_idx
    ON audit_events (entity_type, entity_id, occurred_at DESC, id DESC);

-- The audit trail is append-only: updates and deletes are rejected at the
-- database level so even a compromised application cannot rewrite history.
CREATE FUNCTION audit_events_reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$;

CREATE TRIGGER audit_events_immutable
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_reject_mutation();
