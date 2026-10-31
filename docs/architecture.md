# Architecture

## Goals

- Keep business rules independent of HTTP and storage.
- Use the Go standard library for HTTP, logging, and SQL (`database/sql`). The
  only runtime dependency is the pure-Go `pgx` database driver.
- Make failure modes explicit (typed errors, problem documents, timeouts).
- Make the HTTP layer testable end-to-end without a live database.

## Layers

```
cmd/api
  └─ internal/transport/httpapi        (HTTP adapter)
       ├─ handler                      request/response mapping
       ├─ middleware                   cross-cutting concerns
       └─ response                     JSON + RFC 9457 helpers
  └─ internal/application/procurement  (use cases)
  └─ internal/domain/procurement       (aggregate + repository port)
       └─ internal/infrastructure/memory   (local adapter)
       └─ internal/infrastructure/postgres (production adapter)
  └─ internal/platform                 (IDs, request IDs, logging)
  └─ internal/config                   (environment configuration)
```

Dependencies point inward:

- `domain` imports only the standard library and `internal/domain`.
- `application` depends on the `domain` repository interface, never on an adapter.
- `transport` depends on `application` and `domain` types.
- `infrastructure` implements domain ports.

## Domain model

`procurement.Procurement` is the aggregate root:

| Field | Notes |
| --- | --- |
| `ID` | `prc_` + 32 hex characters |
| `Number` | `PR-YYYYMM-XXXXXXXXXXXXXXXX`, human reference (64-bit entropy, unique) |
| `Amount` | `int64` minor units; avoids float rounding |
| `RequesterID` | authenticated subject that created the request (optional today) |
| `Currency` | ISO 4217, normalized to upper case |
| `Status` | `draft`, `submitted`, `approved`, `rejected`, `cancelled` |
| `Version` | optimistic concurrency token |

Invariants are enforced in `Validate()`; transitions are enforced by
`CanTransition` and the transition methods. A failed transition returns
`domain.ErrInvalidTransition` and leaves the aggregate unchanged.

## Application layer

`application/procurement.Service` implements:

- `Create`, `Get`, `List`
- `Submit`, `Approve`, `Reject`, `Cancel`

Each mutation reads the aggregate, records the expected version, applies the
domain transition, and writes with optimistic locking. A concurrent write
surfaces as `domain.ErrConflict` (HTTP 409).

## Persistence port

```go
type Repository interface {
    Create(ctx context.Context, p Procurement) error
    GetByID(ctx context.Context, id string) (Procurement, error)
    Update(ctx context.Context, p Procurement, expectedVersion int) error
    List(ctx context.Context, filter ListFilter) (ListResult, error)
}
```

Two adapters implement the port:

- `infrastructure/memory` — thread-safe reference implementation for local
  development and tests.
- `infrastructure/postgres` — `database/sql` adapter with optimistic updates
  (`UPDATE ... WHERE id = $1 AND version = $2`), unique constraints on `id` and
  `number`, and `ILIKE` search.

Both adapters run the same contract suite
(`internal/infrastructure/repositorytest`). PostgreSQL integration tests run
when `TEST_DATABASE_URL` is set and are skipped otherwise.

`postgres.UnitOfWork` puts a `*sql.Tx` in the context. Repositories resolve the
transaction from the context, so future use cases can write the aggregate and
its audit events atomically without leaking SQL into the application layer.

### Migrations

`migrations/*.sql` are embedded with `//go:embed` and applied by
`migrations.Up`. The runner takes a PostgreSQL advisory lock, records versions
in `schema_migrations`, and executes each migration in a transaction. Every
migration ships with a matching `.down.sql`. Startup applies migrations when
`DB_AUTO_MIGRATE=true`; `cmd/migrate` exposes `up`, `down`, and `status`.

## HTTP layer

### Routing

Go 1.22 method-aware patterns:

```
GET  /healthz
GET  /readyz
GET  /docs
GET  /openapi.yaml
POST /api/v1/procurements
GET  /api/v1/procurements
GET  /api/v1/procurements/{id}
POST /api/v1/procurements/{id}/submit
POST /api/v1/procurements/{id}/approve
POST /api/v1/procurements/{id}/reject
POST /api/v1/procurements/{id}/cancel
```

`/docs` serves a Scalar API reference bootstrapped from the embedded OpenAPI
3.1 document; `/openapi.yaml` serves the raw contract. Both are registered only
when `DOCS_ENABLED` resolves to true (default: non-production). The docs page
overrides the strict API CSP with a narrow policy that permits only the Scalar
CDN and fonts, so the rest of the API keeps `default-src 'none'`.

`ServeMux` produces plain-text 404/405 responses. A small fallback buffers only
unmatched requests and rewrites them as problem documents, preserving the
`Allow` header for 405s. Matched requests are served through the mux so
`PathValue` is populated.

### Middleware order

```
RequestID → AccessLog → Recover → SecurityHeaders → CORS → Timeout → handler
```

- `RequestID` validates or generates a correlation ID and stores it in context.
- `AccessLog` emits one structured record per request.
- `Recover` converts panics into 500 problems and re-panics `http.ErrAbortHandler`.
- `SecurityHeaders` sets conservative JSON-API headers; HSTS only over TLS.
- `CORS` allows exact origins and answers preflight requests.
- `Timeout` attaches a per-request deadline; handlers and repositories honor it.

### Errors

| Source | Status | Code |
| --- | --- | --- |
| `*response.DecodeError` | as set | as set |
| `*handler.QueryError` | 400 | `INVALID_QUERY` |
| `*domain.ValidationError` | 422 | `VALIDATION_ERROR` |
| `domain.ErrNotFound` | 404 | `NOT_FOUND` |
| `domain.ErrConflict` | 409 | `CONFLICT` |
| `domain.ErrInvalidTransition` | 409 | `INVALID_TRANSITION` |
| `context.DeadlineExceeded` | 504 | `TIMEOUT` |
| `context.Canceled` | 408 | `REQUEST_CANCELED` |
| unknown | 500 | `INTERNAL_ERROR` |

Unknown errors are logged with the request ID and never expose internal detail.

## Server lifecycle

1. Load and validate configuration (production requires `DATABASE_URL`).
2. Open PostgreSQL and apply migrations, or fall back to the in-memory adapter.
3. Build logger, service, health checks, router.
4. `net.Listen` first so binding errors surface before serving.
5. `Serve` in a goroutine.
6. On `SIGINT`/`SIGTERM`, call `Shutdown` with a bounded context.
7. Return after in-flight requests drain or the budget expires.

## Testing strategy

- Domain: table-driven invariant and transition tests.
- Application: fake repository asserting calls and version expectations.
- Infrastructure: a shared repository contract run against memory and
  PostgreSQL, covering concurrency, uniqueness, filtering, pagination, and
  context cancellation. PostgreSQL tests are gated by `TEST_DATABASE_URL`.
- Transport: end-to-end `httptest` coverage of status codes, envelopes, and
  problem documents; server lifecycle tests with real listeners.
- `go test -race ./...` is part of the definition of done; CI runs it against a
  PostgreSQL service container.
