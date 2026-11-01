# Architecture

## Goals

- Keep business rules independent of HTTP and storage.
- Use the Go standard library for HTTP, logging, and SQL (`database/sql`).
  Runtime dependencies are limited to the drivers and clients that speak the
  infrastructure protocols: pure-Go `pgx` for PostgreSQL, `go-redis` for Redis,
  and `amqp091-go` for RabbitMQ.
- Make failure modes explicit (typed errors, problem documents, timeouts).
- Make the HTTP layer testable end-to-end without a live database.

## Layers

```
cmd/api                                HTTP entrypoint + outbox relay
cmd/worker                             asynchronous consumer entrypoint
cmd/migrate                            migration CLI
  └─ internal/transport/httpapi        (HTTP adapter)
       ├─ handler                      request/response mapping
       ├─ middleware                   cross-cutting concerns
       └─ response                     JSON + RFC 9457 helpers
  └─ internal/application/procurement  (use cases, cache port, event emission)
  └─ internal/domain/procurement       (aggregate + repository port)
  └─ internal/domain/audit             (append-only audit port)
  └─ internal/domain/event             (event envelope)
  └─ internal/domain/outbox            (transactional outbox port)
       └─ internal/infrastructure/memory   (local adapters)
       └─ internal/infrastructure/postgres (production adapters)
       └─ internal/infrastructure/redis   (limiter client + JSON cache)
       └─ internal/infrastructure/rabbitmq (publisher + consumers)
  └─ internal/platform                 (IDs, request IDs, logging, auth,
                                        rate limiting, metrics, tracing, relay)
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
transaction from the context, so the use cases write the aggregate and its
audit events atomically without leaking SQL into the application layer.

## Authentication and authorization

`internal/platform/auth` verifies credentials and produces a `Principal`:

- static API keys compared with `crypto/subtle.ConstantTimeCompare`;
- HS256 JWTs with the algorithm pinned to `HS256`, plus `exp`, `nbf`, `iss`,
  `aud`, and role validation.

The HTTP authentication middleware runs only for `/api/v1/*`. It maps the
principal to an application `Actor` and stores it in the request context.
Health probes and API documentation bypass it. When `AUTH_ENABLED=false`, the
router injects a local admin actor instead.

The application policy fails closed: no actor means `domain.ErrForbidden`.

| Action | Rule |
| --- | --- |
| Create | `admin` or `requester` |
| Get / List / Audit | any authenticated actor |
| Submit | owner (`requester_id`) or `admin` |
| Approve / Reject | `admin`, or `approver` whose department matches the request; never the requester themselves |
| Cancel | owner with the `requester` role, or `admin` |

`requester_id` is derived from the token subject, never from client input.

## Audit trail

`internal/domain/audit` defines an append-only port. Every create, submit,
approve, reject, and cancel appends one event with the actor, role, status
transition, metadata, and timestamp. The application records the event inside
the same unit of work as the aggregate write.

On PostgreSQL, `audit_events` has a trigger that rejects `UPDATE` and `DELETE`,
so history cannot be rewritten even by a compromised application. Events are
read newest first through `GET /api/v1/procurements/{id}/audit`, paginated with
`page` and `page_size` (default 20, maximum 100).

## Idempotency

`internal/domain/idempotency` defines a claim/replay port implemented by the
memory and PostgreSQL adapters. `POST /api/v1/procurements` is wrapped by
middleware that:

1. hashes the method, URI, and body, scoped to the authenticated actor;
2. atomically claims `(actor_id, idempotency_key)` with `ON CONFLICT DO
   NOTHING`;
3. stores the status, content type, `Location`, and body on success;
4. replays the stored response for an identical retry, rejects a different body
   with `422 IDEMPOTENCY_KEY_REUSED`, and reports `409
   IDEMPOTENCY_IN_PROGRESS` while the first request runs;
5. releases the claim when the handler returns a 5xx or panics.

Claims expire after `IDEMPOTENCY_TTL`; an expired claim can be taken over
atomically. Keys are never shared across actors.

## Rate limiting and observability

`internal/platform/ratelimit` is a token bucket keyed by authenticated subject
or remote IP. `middleware.AuthenticatedRateLimit` authenticates first and then
consumes the subject's bucket; failed authentication consumes the IP's bucket
before the 401 is written, so credential guessing is throttled. It emits
`X-RateLimit-*` headers and returns `429 RATE_LIMITED` with `Retry-After`.

`internal/platform/metrics` is a dependency-free registry that renders
Prometheus text format at `GET /metrics`. The route label is the `ServeMux`
pattern, keeping cardinality bounded. `internal/platform/tracing` parses and
emits W3C `traceparent`, stores trace/span IDs in context, and feeds them into
access logs; span export to an OpenTelemetry backend is not yet implemented.

### Redis

When `REDIS_URL` is set the API creates one `go-redis` client and derives two
independent capabilities from it:

- **Shared rate limiting.** `ratelimit.RedisLimiter` runs a token bucket as a
  Lua script (`HMGET`/`HSET`/`PEXPIRE`), so refill and consume are atomic across
  replicas. `ratelimit.Fallback` keeps an in-process `Memory` bucket and uses it
  if Redis errors, and the middleware allows traffic if both fail. Limits stay
  advisory during an outage instead of taking the API down.
- **Read-through cache.** `Redis.Cache` stores JSON under versioned keys and
  treats decode errors as misses (deleting the corrupt entry). Each entry has a
  generation marker; mutations advance it after commit, and a read only
  repopulates the cache if the generation it observed before the database read
  is still current. That fence prevents a slow read from caching a pre-mutation
  aggregate, while `CACHE_TTL` still bounds retention.

Redis is never a source of truth: every cached value is reconstructible from
PostgreSQL and every rate-limit decision degrades to local state.

### Events: outbox, relay, worker

The write path never talks to RabbitMQ directly. A lifecycle change writes

1. the aggregate row,
2. the audit event, and
3. an `outbox_events` row

inside one transaction. This is the transactional outbox pattern: either all
three commit or none do, so an event can never describe a change that rolled
back and a committed change can never lose its event.

`internal/platform/relay` polls due rows:

1. `Claim` selects unpublished rows whose backoff elapsed and leases them with
   `FOR UPDATE SKIP LOCKED`, moving `next_attempt_at` forward by the lease.
2. Each event is published to the durable topic exchange in confirm mode.
3. Success sets `published_at`; failure increments `attempts`, records
   `last_error`, and schedules an exponential backoff (1s … 5m). A crash after
   publish but before step 3 redelivers the event, so consumers must be
   idempotent: delivery is at-least-once.

`cmd/worker` declares durable queues against the exchange and acknowledges
manually:

| Queue | Routing keys | Handler |
| --- | --- | --- |
| `procure.notifications` | `procurement.*` | notify the requester (webhook or log) |
| `procure.integrations` | `procurement.approved` | ERP sync (webhook or log) |

A handler is retried three times with exponential backoff; afterwards the
message is dead-lettered to `<queue>.dlq` via `<queue>.dlx`. Without a configured
broker URL the service does not write outbox rows at all, so an API-only
deployment accumulates nothing.

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
GET  /api/v1/procurements/{id}/audit
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

`/readyz` gates on required checks only (the database). Redis and RabbitMQ are
reported in `checks`; their failure returns `200` with `"status":"degraded"`,
so a lost optimization does not take every replica out of rotation.

`ServeMux` produces plain-text 404/405 responses. A small fallback buffers only
unmatched requests and rewrites them as problem documents, preserving the
`Allow` header for 405s. Matched requests are served through the mux so
`PathValue` is populated.

### Middleware order

```
Timeout → RequestID → Tracing → AccessLog → Metrics → Recover
        → SecurityHeaders → CORS → handler
```

- `Timeout` is outermost so every layer and the `ServeMux` share one request
  value (metrics read `r.Pattern` from it).
- `RequestID` validates or generates a correlation ID and stores it in context.
- `Tracing` adopts or creates W3C trace context and emits `traceparent` and
  `X-Trace-ID`.
- `AccessLog` emits one structured record per request, including trace IDs.
- `Metrics` records counters and duration histograms by route pattern.
- `Recover` converts panics into 500 problems and re-panics `http.ErrAbortHandler`.
- `SecurityHeaders` sets conservative JSON-API headers; HSTS only over TLS.
- `CORS` allows exact origins and answers preflight requests.

### Errors

| Source | Status | Code |
| --- | --- | --- |
| `*response.DecodeError` | as set | as set |
| `*handler.QueryError` | 400 | `INVALID_QUERY` |
| `*domain.ValidationError` | 422 | `VALIDATION_ERROR` |
| `domain.ErrNotFound` | 404 | `NOT_FOUND` |
| `domain.ErrForbidden` | 403 | `FORBIDDEN` |
| `auth.ErrUnauthenticated` (middleware) | 401 | `UNAUTHENTICATED` |
| `domain.ErrConflict` | 409 | `CONFLICT` |
| idempotency in progress | 409 | `IDEMPOTENCY_IN_PROGRESS` |
| idempotency key reused | 422 | `IDEMPOTENCY_KEY_REUSED` |
| rate limit exceeded | 429 | `RATE_LIMITED` |
| `domain.ErrInvalidTransition` | 409 | `INVALID_TRANSITION` |
| `context.DeadlineExceeded` | 504 | `TIMEOUT` |
| `context.Canceled` | 408 | `REQUEST_CANCELED` |
| unknown | 500 | `INTERNAL_ERROR` |

Unknown errors are logged with the request ID and never expose internal detail.

## Server lifecycle

1. Load and validate configuration (production requires `DATABASE_URL` and
   authentication).
2. Open PostgreSQL and apply migrations, or fall back to the in-memory adapters.
3. Create the Redis client (if configured) and assemble the cache and limiter.
4. Create the RabbitMQ publisher (if configured), register its optional health
   check, and start the outbox relay goroutine.
5. Build the credential chain, metrics registry, logger, service, health checks,
   and router.
6. `net.Listen` first so binding errors surface before serving.
7. `Serve` in a goroutine.
8. On `SIGINT`/`SIGTERM`, call `Shutdown` with a bounded context.
9. Wait for the outbox relay goroutine to stop, then close the publisher and
   database, after in-flight requests drain or the budget expires.

## Testing strategy

- Domain: table-driven invariant and transition tests.
- Application: fake repository asserting calls and version expectations, plus
  policy tests for every role and department/ownership combination.
- Authentication: API-key and JWT acceptance/rejection tables, including
  expired, wrong-audience, bad-signature, malformed, and `alg: none` tokens.
- Idempotency: repository claim/replay contract against memory and PostgreSQL,
  plus middleware tests for replay, key reuse, in-progress, 5xx release, and
  actor isolation.
- Rate limiting and metrics: token-bucket refill/pruning, Redis Lua bucket
  atomicity across clients, fallback behaviour, 429 mapping, and Prometheus
  rendering with bounded route labels.
- Caching: hit/miss behaviour, invalidation, generation fencing against stale
  writes, and graceful degradation on cache errors (fake cache), plus Redis
  set/get/TTL/corruption tests.
- Outbox: one shared contract for memory and PostgreSQL covering enqueue,
  ordered claiming, lease expiry, publish, backoff, and pruning; relay tests
  cover publish success, failure scheduling, and shutdown.
- Events: the service emits the right type and payload per transition and
  fails the transaction when the outbox write fails.
- Messaging: publisher confirm round-trip against RabbitMQ, consumer
  ack/retry/dead-letter decisions, and webhook handler payloads.
- Infrastructure: a shared repository contract run against memory and
  PostgreSQL, covering concurrency, uniqueness, filtering, pagination, and
  context cancellation. PostgreSQL, Redis, and RabbitMQ tests are gated by
  `TEST_DATABASE_URL`, `TEST_REDIS_URL`, and `TEST_RABBITMQ_URL`.
- Transport: end-to-end `httptest` coverage of status codes, envelopes, and
  problem documents; server lifecycle tests with real listeners.
- `go test -race ./...` is part of the definition of done; CI runs it against
  PostgreSQL, Redis, and RabbitMQ service containers.
