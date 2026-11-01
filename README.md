# procure-flow

Enterprise procurement workflow API built on the Go standard library
(`net/http`, Go 1.22+ routing, `log/slog`, `database/sql`). There is no
third-party web framework; PostgreSQL is reached through the pure-Go `pgx`
driver. Redis provides shared rate limiting and read caching, and RabbitMQ
carries lifecycle events to an asynchronous worker.

## Features

- Layered architecture: domain → application → infrastructure/transport.
- Procurement aggregate with an explicit status state machine.
- Optimistic concurrency via a monotonically increasing `Version`.
- PostgreSQL persistence with embedded, reversible migrations, plus an
  in-memory adapter for local development and tests.
- RFC 9457 `application/problem+json` error responses.
- Strict JSON decoding: 1 MiB limit, unknown fields rejected, `application/json` required.
- Request correlation (`X-Request-ID`), structured access logs, panic recovery.
- Security headers, exact-origin CORS, per-request timeout.
- Liveness/readiness probes with pluggable dependency checks.
- Graceful shutdown on `SIGINT`/`SIGTERM`.
- Authentication with static API keys or HS256 JWTs (algorithm pinned).
- Role-based access control: `admin`, `approver`, `requester`, and `viewer`,
  with department scoping for approvers and ownership checks for requesters.
- Append-only audit trail written in the same transaction as each change, with
  a database trigger that rejects updates and deletes.
- Idempotent writes via `Idempotency-Key`, scoped per actor, with replay of the
  stored response and rejection of key reuse with a different body.
- Per-caller token-bucket rate limiting with `Retry-After` and rate-limit
  headers; optionally shared across replicas through Redis, with an in-process
  fallback when Redis is unavailable.
- Read-through Redis cache for single-procurement reads, invalidated on every
  write and bounded by a TTL.
- Transactional outbox + RabbitMQ topic exchange: lifecycle events are
  published with confirms, and a worker performs requester notifications and
  ERP syncs without blocking the HTTP response.
- Prometheus metrics at `/metrics` and W3C Trace Context propagation.
- Scalar API reference (OpenAPI 3.1) and an HTTP request collection.
- CI that runs `gofmt`, `go vet`, race tests against PostgreSQL, Redis, and
  RabbitMQ, and a build.

## Requirements

- Go 1.26+
- Docker (optional, for PostgreSQL, Redis, RabbitMQ, and container images)

## Quick start

### In-memory (fastest)

```bash
make run
```

The API listens on `:8080` by default and stores data in memory. Data is lost
when the process exits, and `APP_ENV=production` refuses to start without a
database.

```bash
curl -s -X POST http://localhost:8080/api/v1/procurements \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Laptop procurement for engineering",
    "description": "20 units of development laptops",
    "department": "Engineering",
    "requester": "Amir",
    "amount": 250000000,
    "currency": "IDR"
  }'

curl -s http://localhost:8080/api/v1/procurements
curl -s -X POST http://localhost:8080/api/v1/procurements/<id>/submit
curl -s -X POST http://localhost:8080/api/v1/procurements/<id>/approve
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
```

### PostgreSQL

```bash
make db-up          # PostgreSQL 18 on 127.0.0.1:55432
cp .env.example .env
# uncomment DATABASE_URL in .env
set -a && . ./.env && set +a
make run            # migrations run automatically on startup
```

The compose stack (`make db-up` then `docker compose -f deployments/docker-compose.yml up api`)
runs the API and database together.

### Redis and RabbitMQ (asynchronous stack)

Redis and RabbitMQ are optional. Start them locally and point the API at them:

```bash
make deps-up        # PostgreSQL :55432, Redis :56379, RabbitMQ :5672
export REDIS_URL=redis://127.0.0.1:56379/0
export RABBITMQ_URL=amqp://guest:guest@127.0.0.1:5672/
export RATE_LIMIT_STORE=redis
make run
make worker         # in a second terminal
```

The worker logs one notification per lifecycle event and simulates the ERP sync
unless webhooks are configured. To run the whole stack in containers, including
the worker:

```bash
docker compose -f deployments/docker-compose.yml up --build
```

## Configuration

Configuration is read from environment variables. See
[`.env.example`](.env.example) and [`configs/config.example.env`](configs/config.example.env).

```bash
cp .env.example .env
set -a && . ./.env && set +a
make run
```

| Variable | Default | Description |
| --- | --- | --- |
| `APP_ENV` | `development` | `development`, `staging`, or `production` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `HTTP_ADDR` | `:8080` | Listen address |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Header read timeout |
| `HTTP_READ_TIMEOUT` | `15s` | Full request read timeout |
| `HTTP_WRITE_TIMEOUT` | `30s` | Response write timeout |
| `HTTP_IDLE_TIMEOUT` | `60s` | Keep-alive idle timeout |
| `HTTP_REQUEST_TIMEOUT` | `20s` | Per-request context deadline |
| `HTTP_SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown budget |
| `HTTP_MAX_HEADER_BYTES` | `1048576` | Maximum request header size |
| `DATABASE_URL` | *(empty)* | PostgreSQL URL; empty selects memory. Required in production |
| `DB_MAX_OPEN_CONNS` | `25` | Maximum open connections |
| `DB_MAX_IDLE_CONNS` | `25` | Maximum idle connections |
| `DB_CONN_MAX_LIFETIME` | `30m` | Connection maximum lifetime |
| `DB_CONN_MAX_IDLE_TIME` | `5m` | Connection maximum idle time |
| `DB_AUTO_MIGRATE` | `true` | Apply embedded migrations on startup |
| `CORS_ALLOWED_ORIGINS` | *(empty)* | Comma-separated exact origins |
| `AUTH_ENABLED` | `true` in staging/production, `false` in development | Require authentication |
| `AUTH_API_KEYS` | *(empty)* | JSON array of static API keys |
| `AUTH_JWT_SECRET` | *(empty)* | HS256 secret, at least 32 characters |
| `AUTH_JWT_ISSUER` | `procure-flow` | Required JWT `iss` |
| `AUTH_JWT_AUDIENCE` | `procure-flow-api` | Required JWT `aud` |
| `AUTH_JWT_LEEWAY` | `30s` | Clock-skew tolerance |
| `IDEMPOTENCY_ENABLED` | `true` | Replay protection for write endpoints |
| `IDEMPOTENCY_TTL` | `24h` | How long stored responses remain replayable |
| `RATE_LIMIT_ENABLED` | `true` in staging/production, `false` in development | Enable per-caller limiting |
| `RATE_LIMIT_RPS` | `20` | Sustained requests per second per caller |
| `RATE_LIMIT_BURST` | `40` | Burst allowance per caller |
| `METRICS_ENABLED` | `true` | Serve Prometheus metrics at `/metrics` |
| `REDIS_URL` | *(empty)* | `redis://` or `rediss://` URL; enables shared state |
| `REDIS_DIAL_TIMEOUT` | `2s` | Redis connect and pool timeout |
| `REDIS_COMMAND_TIMEOUT` | `1s` | Redis read/write command timeout |
| `CACHE_ENABLED` | `true` when `REDIS_URL` is set | Read-through cache for GET by id |
| `CACHE_TTL` | `30s` | Maximum staleness bound for cached aggregates |
| `RATE_LIMIT_STORE` | `auto` | `auto`, `memory`, or `redis`; redis requires `REDIS_URL` |
| `RABBITMQ_URL` | *(empty)* | `amqp://` or `amqps://` URL; enables async events |
| `RABBITMQ_EXCHANGE` | `procure.events` | Durable topic exchange for lifecycle events |
| `NOTIFICATION_WEBHOOK_URL` | *(empty)* | Worker POSTs requester notifications here |
| `ERP_WEBHOOK_URL` | *(empty)* | Worker POSTs approved procurements here |
| `DOCS_ENABLED` | `true` (non-production) | Serve `/docs` and `/openapi.yaml` |

`HTTP_REQUEST_TIMEOUT` must not exceed `HTTP_WRITE_TIMEOUT`. Wildcard CORS
origins are rejected. `DB_MAX_IDLE_CONNS` must not exceed `DB_MAX_OPEN_CONNS`.
Production requires `DATABASE_URL`, `AUTH_ENABLED=true`, and at least one of
`AUTH_API_KEYS` or `AUTH_JWT_SECRET`. `RATE_LIMIT_STORE=redis` requires
`REDIS_URL`, and webhook URLs must use HTTP or HTTPS.

## Authentication and authorization

`/healthz`, `/readyz`, `/metrics`, `/docs`, and `/openapi.yaml` are public. Every
`/api/v1` route requires `Authorization: Bearer <token>`. A static API key or an
HS256 JWT is accepted. JWTs must carry `sub`, `exp`, a valid `roles` claim, and
match the configured issuer and audience.

```bash
export AUTH_ENABLED=true
export AUTH_API_KEYS='[{"token":"local-admin-token-0123456789","subject":"user_admin","name":"Local Admin","roles":["admin"]}]'
make run

curl -s http://localhost:8080/api/v1/procurements \
  -H 'Authorization: Bearer local-admin-token-0123456789'
```

| Role | Permissions |
| --- | --- |
| `admin` | Everything, including cross-department approval and ownership overrides |
| `approver` | Approve or reject submitted requests in the actor's department |
| `requester` | Create requests; submit and cancel requests they own |
| `viewer` | Read requests and audit trails |

Ownership is recorded as `requester_id`, derived from the token subject rather
than the request body. Authorization fails closed: a missing actor is treated
as forbidden. When `AUTH_ENABLED=false`, the router injects a local admin actor
so development keeps working without credentials.

### Audit trail

Every create, submit, approve, reject, and cancel writes an immutable event.
On PostgreSQL the aggregate and the audit row share one transaction, so a
failed audit write rolls back the change. Read the trail with:

```bash
curl -s 'http://localhost:8080/api/v1/procurements/<id>/audit?page=1&page_size=20' \
  -H 'Authorization: Bearer local-admin-token-0123456789'
```

Events are returned newest first with `meta.page`, `meta.page_size`,
`meta.total`, and `meta.total_pages`.

### Idempotency

`POST /api/v1/procurements` accepts an `Idempotency-Key` header. Keys are scoped
per authenticated actor and stored for `IDEMPOTENCY_TTL`. A retry with the same
key and body replays the original status, body, and `Location` header, marked
with `Idempotent-Replayed: true`. Reusing a key with a different body returns
`422 IDEMPOTENCY_KEY_REUSED`; a concurrent retry while the first request is
still running returns `409 IDEMPOTENCY_IN_PROGRESS` with `Retry-After: 1`.
Responses with a 5xx status release the claim so the client can retry.

```bash
curl -s -X POST http://localhost:8080/api/v1/procurements \
  -H 'Authorization: Bearer local-admin-token-0123456789' \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 5b1c2a7e-create-1' \
  -d '{"title":"Laptop procurement","department":"Engineering","amount":250000000,"currency":"IDR"}'
```

### Rate limiting

Every `/api/v1` route is limited per caller: the authenticated subject when
present, otherwise the remote IP address. The token bucket allows
`RATE_LIMIT_BURST` immediate requests and refills at `RATE_LIMIT_RPS`. Responses
carry `X-RateLimit-Limit` and `X-RateLimit-Remaining`; an exhausted bucket
returns `429 RATE_LIMITED` with `Retry-After` in seconds.

With `RATE_LIMIT_STORE=redis` (or `auto` plus `REDIS_URL`) the bucket lives in
Redis and is updated by an atomic Lua script, so all API replicas share one
limit. If Redis fails, the middleware falls back to a per-process bucket and
keeps serving traffic; if both backends fail, requests are allowed and the
failure is logged.

### Caching

`GET /api/v1/procurements/{id}` uses cache-aside: a miss loads the aggregate
from PostgreSQL and stores it as JSON in Redis for `CACHE_TTL`; create, submit,
approve, reject, and cancel delete the key after the transaction commits. Cache
errors are treated as misses and never fail a request, and a short TTL bounds
staleness if an invalidation races with a concurrent read.

Keys are versioned (`procurement:v1:<id>`) so an incompatible aggregate change
can be rolled out by bumping the prefix. List results are not cached because
filter and pagination combinations would explode the key space.

### Metrics and tracing

`GET /metrics` exposes Prometheus text format:

- `http_requests_total{method,route,status}`
- `http_request_duration_seconds{method,route,status}` histogram

The route label is the `ServeMux` pattern (for example
`GET /api/v1/procurements/{id}`), so identifiers never inflate cardinality.
The endpoint is public like the probes; restrict it at the network edge.

Requests accept and emit W3C `traceparent`; `X-Trace-ID` echoes the trace
identifier. Access logs include `trace_id` and `span_id`. Exporting spans to an
OpenTelemetry backend is on the roadmap.

### Asynchronous events (RabbitMQ)

When `RABBITMQ_URL` is set, every lifecycle change writes an outbox row in the
same transaction as the aggregate update and audit event. A relay inside the API
leases due rows (`FOR UPDATE SKIP LOCKED`), publishes the event envelope to the
topic exchange in confirm mode, and marks the row published. Failures back off
from 1s to 5m and are retried; a crash after publish but before the mark causes
a duplicate, so delivery is at-least-once. Without a broker URL, no outbox rows
are written at all.

Routing keys are the event types (`procurement.created`, `submitted`,
`approved`, `rejected`, `cancelled`). `cmd/worker` declares two durable queues:

- `procure.notifications` binds every lifecycle key and notifies the requester.
- `procure.integrations` binds `procurement.approved` and syncs with the ERP.

Handlers retry three times with exponential backoff and are then dead-lettered
to `<queue>.dlq` through `<queue>.dlx`. Notification and ERP payloads include
the `event_id`, so external systems can deduplicate redeliveries. When a webhook
is not configured the worker logs the action, which keeps the flow runnable
locally. Run it with `make worker` (or the `worker` compose service):

```bash
RABBITMQ_URL=amqp://guest:guest@127.0.0.1:5672/ \
NOTIFICATION_WEBHOOK_URL=https://hooks.example.com/notify \
ERP_WEBHOOK_URL=https://erp.example.com/procurements \
make worker
```

The event envelope is `{id, type, aggregate_type, aggregate_id, actor_id,
occurred_at, payload}`; the payload carries a procurement snapshot (`number`,
`title`, `department`, `requester`, `amount`, `currency`, `status`,
`from_status`, `version`, `actor`).

## Database and migrations

Migrations live in [`migrations/`](migrations) as reversible `.up.sql` /
`.down.sql` pairs and are embedded into the binary. The runner takes a
PostgreSQL advisory lock, records applied versions in `schema_migrations`, and
applies each migration in its own transaction.

```bash
make migrate-up      DATABASE_URL='postgres://...'   # apply pending
make migrate-status  DATABASE_URL='postgres://...'   # show applied/pending
go run ./cmd/migrate down                            # roll back the last one
```

Set `DB_AUTO_MIGRATE=false` when migrations are applied by a deployment step
instead of application startup. The container image ships the same runner as
`/migrate`, so a deployment job can run `/migrate up` against the database.

## Development

```bash
make test              # go test ./... (integration tests skip without services)
make test-race         # go test -race ./...
make test-integration  # race tests against PostgreSQL, Redis, and RabbitMQ
make deps-up           # start PostgreSQL, Redis, and RabbitMQ
make db-up             # start only PostgreSQL
make db-reset          # destroy the database volume and recreate
make worker            # run the async worker (needs RABBITMQ_URL)
make cover             # coverage report
make vet               # go vet ./...
make fmt               # gofmt -w
make build             # binary in bin/
```

Tests are written first (TDD) and live next to the code they exercise. The
shared repository contract in `internal/infrastructure/repositorytest` runs
against both the in-memory and PostgreSQL adapters.

## Project layout

```
cmd/api/                       application entrypoint
cmd/worker/                    async notification and ERP worker
cmd/migrate/                   migration CLI (up, down, status)
internal/config/               environment configuration
internal/domain/               shared domain errors
internal/domain/procurement/   aggregate, state machine, repository port
internal/domain/event/         event envelope
internal/domain/outbox/        transactional outbox port
internal/application/          use-case services, cache port, event emission
internal/infrastructure/       memory + PostgreSQL + Redis + RabbitMQ adapters
internal/platform/             request IDs, ID generator, logging, rate limiting,
                               metrics, tracing, outbox relay
internal/transport/httpapi/    router, handlers, middleware, response helpers
api/                           OpenAPI contract + HTTP request collection
configs/                       example configuration
deployments/                   container/deployment assets
docs/                          architecture notes
migrations/                    embedded, reversible SQL migrations
scripts/                       operational scripts
```

## API

The contract lives in [`api/openapi.yaml`](api/openapi.yaml). All success
responses use an envelope:

```json
{ "data": { }, "meta": { } }
```

All errors use RFC 9457:

```json
{
  "type": "about:blank",
  "title": "Validation failed",
  "status": 422,
  "detail": "one or more fields are invalid",
  "instance": "/api/v1/procurements",
  "code": "VALIDATION_ERROR",
  "request_id": "…",
  "errors": { "title": "must be between 3 and 200 characters" }
}
```

### Interactive API reference (Scalar)

Open <http://localhost:8080/docs> after `make run`. The server embeds the
OpenAPI 3.1 document and serves a [Scalar](https://scalar.com) API reference —
the modern successor to Swagger UI. It supports dark mode, code samples in
multiple languages, and an in-browser request client so you can exercise every
endpoint without installing anything.

- `/docs` — interactive reference with a built-in request client
- `/openapi.yaml` — raw contract (also importable into Bruno, Postman,
  Hoppscotch, Insomnia, or `openapi-generator`)
- Disable in production with `DOCS_ENABLED=false` (the default there).

### Testing the API

| Tool | How |
| --- | --- |
| Scalar | `make run`, then <http://localhost:8080/docs> → “Test Request” |
| HTTP file | Open [`api/requests.http`](api/requests.http) in VS Code (REST Client) or JetBrains |
| Smoke script | `make smoke` (or `BASE_URL=… make smoke`) |
| Go tests | `make test-integration` — end-to-end `httptest` plus PostgreSQL contract tests |
| curl | `curl -s localhost:8080/api/v1/procurements` |

`api/requests.http` includes happy paths, the full workflow, and negative cases
(400 invalid JSON, 404, 405, 415, 422).

## Status workflow

```
draft ──submit──▶ submitted ──approve──▶ approved
  │                   │
  │                   └──reject──▶ rejected
  └──cancel───────────┴──cancel──▶ cancelled
```

## Container

```bash
make docker-build
docker run --rm -p 8080:8080 \
  -e APP_ENV=production \
  -e DATABASE_URL='postgres://user:pass@host:5432/procure?sslmode=disable' \
  -e AUTH_ENABLED=true \
  -e AUTH_API_KEYS='[{"token":"replace-me-0123456789","subject":"user_admin","name":"Admin","roles":["admin"]}]' \
  procure-flow:local
```

The image contains both binaries: `/api` is the default entrypoint and
`/worker` runs the asynchronous consumers. Or run the full stack, including
Redis, RabbitMQ, and the worker:

```bash
docker compose -f deployments/docker-compose.yml up --build
```

## Roadmap

- OIDC discovery and key rotation for JWT verification.
- OpenTelemetry span export (trace context propagation already exists).
- Kubernetes manifests, broker-level dead-letter replay tooling, backup and
  retention policy, and load/fuzz tests.
