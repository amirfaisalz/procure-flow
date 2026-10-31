# Migrations

This directory is reserved for database migrations once a SQL repository
adapter is introduced.

## Conventions

- Use sequential, zero-padded filenames: `0001_create_procurements.up.sql` and
  `0001_create_procurements.down.sql`.
- Every migration must be reversible.
- Migrations must not depend on application code.
- Apply migrations in a transaction when the database supports transactional
  DDL.
- Never edit an applied migration; add a new one instead.

## Expected schema

The procurement aggregate maps to a table with:

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `text` | primary key |
| `number` | `text` | unique |
| `title` | `text` | |
| `description` | `text` | |
| `department` | `text` | |
| `requester` | `text` | |
| `amount` | `bigint` | minor units |
| `currency` | `char(3)` | ISO 4217 |
| `status` | `text` | check constraint on allowed values |
| `rejection_reason` | `text` | |
| `created_at` | `timestamptz` | |
| `updated_at` | `timestamptz` | |
| `version` | `integer` | optimistic concurrency |

Optimistic updates must use:

```sql
UPDATE procurements
SET ..., version = version + 1, updated_at = $n
WHERE id = $1 AND version = $2;
```

A zero-row result means the caller must return `domain.ErrConflict`.
