# Migrations

Reversible SQL migrations embedded into the binary and applied by
`migrations.Up` (`internal` runner in this package).

## Conventions

- Sequential, zero-padded filenames: `0001_create_procurements.up.sql` and
  `0001_create_procurements.down.sql`.
- Every migration must be reversible.
- Migrations must not depend on application code.
- Each migration runs in its own transaction.
- Never edit an applied migration; add a new one instead.
- A PostgreSQL advisory lock serialises concurrent migrators.

## Running

```bash
# Apply everything pending
go run ./cmd/migrate up

# Show applied/pending state
go run ./cmd/migrate status

# Roll back the most recent migration
go run ./cmd/migrate down
```

`DATABASE_URL` must be set. The application also applies pending migrations on
startup when `DB_AUTO_MIGRATE=true` (the default).

## Current schema

### `procurements`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `text` | primary key |
| `number` | `text` | unique |
| `title` | `text` | |
| `description` | `text` | |
| `department` | `text` | |
| `requester` | `text` | display name |
| `requester_id` | `text` | authenticated subject |
| `amount` | `bigint` | minor units, `> 0` |
| `currency` | `text` | `^[A-Z]{3}$` |
| `status` | `text` | check constraint on allowed values |
| `rejection_reason` | `text` | |
| `created_at` | `timestamptz` | |
| `updated_at` | `timestamptz` | |
| `version` | `integer` | optimistic concurrency, `> 0` |

Optimistic updates use:

```sql
UPDATE procurements
SET ..., version = version + 1, updated_at = $n
WHERE id = $1 AND version = $2;
```

A zero-row result maps to `domain.ErrConflict` (or `domain.ErrNotFound` when the
row does not exist).

### `audit_events`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `bigint` | identity primary key |
| `entity_type` | `text` | e.g. `procurement` |
| `entity_id` | `text` | aggregate identifier |
| `action` | `text` | e.g. `procurement.approved` |
| `actor_id` | `text` | authenticated subject |
| `actor_name` | `text` | display name |
| `actor_role` | `text` | primary role at the time of the action |
| `from_status` | `text` | previous status, empty on create |
| `to_status` | `text` | resulting status |
| `metadata` | `jsonb` | contextual data |
| `occurred_at` | `timestamptz` | event time |

The table is append-only: a trigger rejects `UPDATE` and `DELETE`.

### `schema_migrations`

Managed by the runner: `version text primary key`, `applied_at timestamptz`.
