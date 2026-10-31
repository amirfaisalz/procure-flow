-- 0001_create_procurements.up.sql
CREATE TABLE procurements (
    id               text        PRIMARY KEY,
    number           text        NOT NULL UNIQUE,
    title            text        NOT NULL,
    description      text        NOT NULL DEFAULT '',
    department       text        NOT NULL,
    requester        text        NOT NULL,
    requester_id     text        NOT NULL DEFAULT '',
    amount           bigint      NOT NULL CHECK (amount > 0),
    currency         text        NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status           text        NOT NULL CHECK (status IN ('draft', 'submitted', 'approved', 'rejected', 'cancelled')),
    rejection_reason text        NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    version          integer     NOT NULL CHECK (version > 0)
);

CREATE INDEX procurements_created_at_idx
    ON procurements (created_at DESC, id ASC);

CREATE INDEX procurements_status_created_at_idx
    ON procurements (status, created_at DESC, id ASC);
