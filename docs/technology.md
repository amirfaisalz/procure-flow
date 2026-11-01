# Technology

What each piece of the stack does in procure-flow, why it is there, and where to
find it. Versions are the ones pinned in `go.mod` and the compose file.

| Technology | Role | Required? | Lives in |
| --- | --- | --- | --- |
| Go 1.26 | Language, HTTP, logging, SQL | Yes | `cmd/`, `internal/` |
| `net/http` | Routing and middleware | Yes | `internal/transport/httpapi` |
| PostgreSQL 18 | System of record | Yes | `internal/infrastructure/postgres`, `migrations/` |
| pgx v5 (stdlib) | PostgreSQL driver | Yes | `internal/infrastructure/postgres/db.go` |
| Redis 7 | Shared rate limiter + read cache | Optional | `internal/infrastructure/redis` |
| RabbitMQ 4 | Lifecycle event delivery | Optional | `internal/infrastructure/rabbitmq` |
| JWT HS256 + bcrypt | Authentication | Yes | `internal/platform/auth`, `internal/platform/password` |
| Prometheus text format | Metrics | Built in | `internal/platform/metrics` |
| W3C Trace Context | Log correlation | Built in | `internal/platform/tracing` |
| OpenAPI 3.1 + Scalar | Contract and docs | Docs optional | `api/`, `internal/transport/httpapi/docs.go` |
| Docker / Compose | Packaging and local stack | Optional | `Dockerfile`, `deployments/` |
| GitHub Actions | CI | Repository | `.github/workflows/ci.yml` |

Two dependency rules shape the project:

1. **The standard library owns the edges.** HTTP, logging, SQL, JSON, and
   cryptography primitives come from Go itself. The only third-party packages
   speak infrastructure protocols: `pgx` (PostgreSQL), `go-redis` (Redis),
   `amqp091-go` (AMQP), and `x/crypto` (bcrypt).
2. **PostgreSQL is the source of truth.** Redis and RabbitMQ are optional
   accelerators; losing either degrades behaviour but does not lose data.

```
                       ┌──────────────┐
   browser / scripts ─▶│   HTTP API   │──▶ PostgreSQL (procurements, audit,
   cookie or bearer    │              │                 users, outbox, idempotency)
                       └──────┬───────┘
                              │ optional
                    ┌─────────┴─────────┐
                    ▼                   ▼
                 Redis 7            RabbitMQ 4
             limiter + cache      topic exchange + queues
                                        │
                                        ▼
                                     worker ──▶ notification / ERP webhooks
```

## Go 1.26

**What it does.** Runs every process: `cmd/api` (HTTP + outbox relay),
`cmd/worker` (consumers), `cmd/migrate` (schema), and `cmd/seed` (accounts).
Wiring is explicit in `cmd/api/main.go`; there is no dependency-injection
framework or global container.

**Why.** A single static binary with a small memory footprint, predictable
concurrency, and a standard library that already covers the service's needs.
`context` carries deadlines and cancellation through HTTP, SQL, and the relay.

**Operational notes.** `CGO_ENABLED=0` for the container image, so binaries are
static and run on distroless. All long-running work takes a context; shutdown
uses `signal.NotifyContext`.

## net/http

**What it does.** Routing, middleware, and the HTTP server. Go 1.22 method
patterns (`POST /api/v1/procurements/{id}/approve`) replace a third-party router;
`{id}` values come from `r.PathValue`.

**Middleware order** (outermost first):

```
Timeout → RequestID → Tracing → AccessLog → Metrics → Recover
        → SecurityHeaders → CORS → auth + rate limit → route handlers
```

- `Timeout` is outermost so every layer sees the same request context.
- `RequestID`/`Tracing` run before the access log so entries can include both.
- `Recover` runs before metrics so a panic still records a status.
- Authentication runs only inside `/api/v1`; probes and docs stay public.

**Why.** Fewer dependencies, no hidden magic, and the handler chain is visible in
`internal/transport/httpapi/router.go`. The tradeoff is writing small helpers
(JSON decoding, problem responses) that a framework would provide.

**Behaviour worth knowing.** JSON decoding is strict: 1 MiB limit, unknown
fields rejected, `Content-Type: application/json` required. Errors are RFC 9457
`application/problem+json`. Server timeouts are configured from the environment,
and graceful shutdown drains in-flight requests before closing dependencies.

## PostgreSQL 18

**What it does.** Stores procurements, audit events, idempotency claims, outbox
rows, and login accounts. It also enforces invariants that should not depend on
application code:

- unique `id` and human-readable `number` on procurements;
- a `version` column for optimistic concurrency;
- a trigger that rejects `UPDATE`/`DELETE` on `audit_events`;
- a unique `(actor_id, idempotency_key)` claim for retries;
- unique lower-case emails on `users`, with roles stored as JSONB.

**Why.** Procurement is transactional and relational: a status change, its audit
row, and its outbox row must commit or fail together. PostgreSQL gives that, plus
`FOR UPDATE SKIP LOCKED` for the outbox relay and mature indexing.

**Transactions.** `postgres.UnitOfWork` places `*sql.Tx` in the context;
repositories resolve it with `querierFrom`, so the application layer commits
aggregate + audit + outbox atomically without importing SQL. Optimistic updates
use `UPDATE ... WHERE id = $1 AND version = $2`; zero rows affected means
`409 CONFLICT`.

**Outbox.** The relay claims due rows oldest-first with `FOR UPDATE SKIP LOCKED`,
leases them for 30s, publishes, and marks them published. Failed publishes back
off up to 5 minutes; published rows are pruned after 7 days. At-least-once
delivery is a deliberate tradeoff — the event `id` is the dedupe key.

**Migrations.** SQL files in `migrations/` are embedded into the binaries
(`go:embed`), named `0005_create_users.up.sql` / `.down.sql`, applied under a
PostgreSQL advisory lock and recorded in `schema_migrations`. `DB_AUTO_MIGRATE`
controls startup migration; the image also ships `/migrate` for deployment jobs
and `cmd/seed` applies migrations before inserting accounts.

## pgx v5

**What it does.** The PostgreSQL driver, used through its `database/sql`
adapter (`sql.Open("pgx", url)`). Queries stay portable `database/sql` calls;
`pgx` supplies the wire protocol, pooling via `puddle`, and type handling.

**Why.** Pure Go, no cgo, fast, and actively maintained. Because the rest of the
code only sees `*sql.DB`, swapping or testing the storage layer does not leak
driver types into repositories.

**Worth knowing.** `[]byte`/`string` JSONB values are encoded explicitly in the
user repository; `roles` is scanned as JSON and unmarshalled rather than relying
on array codecs.

## Redis 7

Redis is optional. When `REDIS_URL` is empty, neither feature below is enabled.

**Shared rate limiting.** A Lua script implements an atomic token bucket keyed by
actor (or IP for failed logins), so every API replica enforces one limit. Without
Redis the limiter runs per process.

**Read-through cache.** `GET /procurements/{id}` stores the aggregate as JSON
for `CACHE_TTL` (30s default). Writes delete the key after commit. Each key has a
generation marker: a read that started before an invalidation cannot write the
old value back, and the Lua script only accepts writes whose generation still
matches.

**Why.** Both are optimizations, not correctness requirements, which is why the
service fails open: cache errors are misses, and limiter errors allow the
request. Redis timeouts (2s dial, 1s command) keep a slow Redis from becoming a
slow API.

## RabbitMQ 4

**What it does.** Carries lifecycle events from the API to the worker:

- one durable topic exchange, `procure.events`;
- `procure.notifications` bound to every lifecycle routing key;
- `procure.integrations` bound to `procurement.approved`;
- publisher confirms on the API side;
- manual ack/nack, prefetch 10, in-process retry (3 attempts, 500ms exponential
  backoff), then dead-letter through `<queue>.dlx` into `<queue>.dlq`;
- reconnect loops with a 5s delay.

**Why.** Notifications and ERP sync must not add latency or failure modes to an
HTTP approval. The transactional outbox guarantees an event is stored whenever
the state change commits; RabbitMQ moves it. Delivery is at-least-once, and
consumers receive `event_id` to deduplicate.

**Without a broker.** No outbox rows are written, no relay runs, and the worker
is not needed. The API is fully functional; side effects are simply absent.

## Authentication: JWT HS256 and bcrypt

**Login.** `application/auth.Service` loads the account, verifies a bcrypt hash
(cost 12), and issues an HS256 JWT with `sub`, `name`, `email`, `roles`,
`department`, `iss`, `aud`, `iat`, `nbf`, and `exp`. The handler writes it to an
`HttpOnly`, `SameSite=Lax`, `/api/v1`-scoped cookie; `Secure` is on outside
development. The token is never returned in the body.

**Verification.** `platform/auth.JWTAuthenticator` pins the algorithm to HS256
(blocking `alg: none` and confusion attacks), checks signature, issuer,
audience, `exp`/`nbf` with 30s leeway, and validates roles. It accepts the token
from the `Authorization` header first and the session cookie second, so API-key
clients and browsers converge on the same `Principal`.

**Why JWT + cookie.** Stateless verification suits a small service with no
session table, while the cookie keeps the token away from JavaScript. Tradeoffs:
logout cannot revoke a copied token before expiry, and cookie auth needs CSRF
care. `SameSite=Lax` plus mandatory `application/json` on mutations (which a
cross-site HTML form cannot send) covers the common CSRF vector.

**Bcrypt.** Passwords are never stored or logged in plaintext. A decoy hash runs
for unknown accounts so login timing does not reveal whether an email exists.
Static API keys are compared with `crypto/subtle` constant-time comparison and
remain supported for service-to-service calls.

**Authorization** lives in the application layer, not in middleware:
`canSubmit`, `canApprove`, and `canCancel` enforce ownership, department scope,
and the no-self-approval rule. The HTTP layer only proves identity.

## Idempotency

`POST /api/v1/procurements` accepts an `Idempotency-Key`. Middleware hashes the
method, URI, `Content-Type`, and body (SHA-256), scoped to the authenticated
actor, and atomically claims the key in PostgreSQL.

- same key + same hash → replay the stored status, headers, and body, with
  `Idempotent-Replayed: true`;
- same key + different hash → `422 IDEMPOTENCY_KEY_REUSED`;
- claim exists but is incomplete → `409 IDEMPOTENCY_IN_PROGRESS` with
  `Retry-After`;
- handler returns 5xx → claim is released;
- success → claim and response are kept for `IDEMPOTENCY_TTL` (24h).

Bodies are capped at 1 MiB by the middleware and again by the JSON decoder.

## Rate limiting

Token bucket per caller: `RATE_LIMIT_BURST` immediate requests, refilling at
`RATE_LIMIT_RPS`. Authenticated requests use `actor:<subject>`; failed
authentication and login use `ip:<remote>` before the 401 is written, so
credential guessing is throttled too. Responses include `X-RateLimit-Limit` and
`X-RateLimit-Remaining`; an empty bucket returns `429 RATE_LIMITED` with
`Retry-After`.

With Redis the bucket is shared and atomic; without it, it is per process. If
both the Redis and local limiters fail, requests are allowed and the error is
logged — availability over strictness for an aid, not an authorization boundary.

## Observability

- **Logging:** `log/slog` JSON with `request_id`, `trace_id`, `span_id`, method,
  path, status, bytes, duration, and remote address. No request bodies or
  credentials are logged.
- **Request IDs:** `X-Request-ID` is preserved when valid, otherwise generated,
  and echoed on every response.
- **Tracing:** `platform/tracing` parses and emits W3C `traceparent`; `X-Trace-ID`
  exposes the trace id. It is propagation only — there is no OpenTelemetry
  exporter yet.
- **Metrics:** `platform/metrics` is a small registry that renders Prometheus
  text without a client library:
  - `http_requests_total{method,route,status}`;
  - `http_request_duration_seconds{method,route,status}` histogram.
  The `route` label is the `ServeMux` pattern, so ids never inflate cardinality.
- **Health:** `/healthz` is liveness; `/readyz` gates only on PostgreSQL.
  Redis/RabbitMQ failures return `200 degraded` with the failing check marked
  `unavailable`, so an optional outage cannot pull every replica from the load
  balancer.

## API contract and documentation

`api/openapi.yaml` is an OpenAPI 3.1 document embedded with `go:embed`. `/docs`
serves a Scalar reference (with an in-browser request client) and
`/openapi.yaml` serves the raw contract, both behind `DOCS_ENABLED` (off in
production by default). `api/requests.http` covers happy paths, the full
workflow, and negative cases for REST Client/JetBrains.

The contract is hand-written rather than generated: handlers and middleware are
small enough that a generated server would add more indirection than it removes.
The tradeoff is that schema changes must be mirrored in both places.

## Testing

- **Repository contract:** `internal/infrastructure/repositorytest` runs the same
  behavioural suite against the PostgreSQL adapter (integration-gated).
- **Test doubles:** `internal/testsupport` provides in-memory repository fakes
  for handler, middleware, and application tests. No command imports it, and CI
  enforces that (`go list -deps ./cmd/...` must not contain it).
- **Gates:** `TEST_DATABASE_URL`, `TEST_REDIS_URL`, and `TEST_RABBITMQ_URL`
  enable the integration suites; without them those tests skip.
- **Race detector:** CI runs `go test -race` against service containers, plus
  `gofmt`, `go mod tidy -diff`, `go vet`, a build, a container build, and the
  layout guard.

## Packaging and deployment

The `Dockerfile` is a multi-stage build: `golang:1.26-alpine` compiles four
static binaries (`/api`, `/worker`, `/migrate`, `/seed`), and
`gcr.io/distroless/static-debian12:nonroot` runs them as a non-root user.
`deployments/docker-compose.yml` wires PostgreSQL, Redis, RabbitMQ, a one-shot
seed service, the API, and the worker with health checks, read-only filesystems,
`no-new-privileges`, and a 30s stop grace period longer than the HTTP shutdown
budget.

The compose stack is a local demo. For production, run the image behind TLS,
keep `AUTH_COOKIE_SECURE=true`, store `AUTH_JWT_SECRET`/`SEED_PASSWORD` in a
secret manager, disable docs unless needed, and decide whether to run migrations
at startup or via `/migrate`.

## Deliberate omissions

- **No web framework or ORM.** The standard library plus explicit repositories
  keep behaviour visible; the cost is more hand-written plumbing.
- **No Kafka.** RabbitMQ covers the event volume and gives DLQs and confirms
  without another operational system.
- **No refresh tokens or server-side sessions.** Re-login after
  `AUTH_SESSION_TTL`; rotate the signing key to invalidate everything.
- **No account-management API.** Accounts are seeded or managed externally;
  there is no registration or password-reset flow yet.
- **No per-record ACL.** Any authenticated actor can read any procurement.
- **Exports for OTel and metrics** are roadmap items; trace propagation and a
  Prometheus endpoint already exist.
