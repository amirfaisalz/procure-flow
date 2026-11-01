-- 0005_create_users.up.sql
-- Login accounts. Passwords are bcrypt hashes; plaintext never reaches storage.
CREATE TABLE users (
    id            text        PRIMARY KEY,
    email         text        NOT NULL UNIQUE,
    name          text        NOT NULL,
    password_hash text        NOT NULL,
    roles         jsonb       NOT NULL,
    department    text        NOT NULL DEFAULT '',
    active        boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_lowercase CHECK (email = lower(email))
);
