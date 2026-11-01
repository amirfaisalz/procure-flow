-- 0002_create_audit_events.down.sql
DROP TRIGGER IF EXISTS audit_events_immutable ON audit_events;
DROP FUNCTION IF EXISTS audit_events_reject_mutation();
DROP TABLE IF EXISTS audit_events;
