<div align="center">

# procure-flow

**Procurement approvals that behave like production — without a web framework.**

Purchase requests with an explicit approval state machine, role-based access,
an append-only audit trail, idempotent writes, and an asynchronous event
pipeline. HTTP, logging, and SQL come from the Go standard library;
PostgreSQL, Redis, and RabbitMQ are used where they earn their keep.

[![CI](https://github.com/amirfaisalz/procure-flow/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/amirfaisalz/procure-flow/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/amirfaisalz/procure-flow.svg)](https://pkg.go.dev/github.com/amirfaisalz/procure-flow)
[![Go version](https://img.shields.io/github/go-mod/go-version/amirfaisalz/procure-flow)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[Quick start](#quick-start) · [Walkthrough](#walkthrough) · [API](#api) ·
[Architecture](docs/architecture.md) · [Configuration](docs/configuration.md) ·
[Contributing](#contributing)

</div>

---

Most procurement examples stop at CRUD. The hard parts start afterwards: two
approvers acting on the same request, a client retrying a `POST` after a
timeout, a webhook that is down, an auditor asking who changed what and when.
`procure-flow` is a compact service that takes those problems seriously — small
enough to read in an afternoon, complete enough to deploy.

## Highlights

- **Explicit lifecycle** — `draft → submitted → approved | rejected | cancelled`,
  enforced by the aggregate rather than scattered `if` statements.
- **Lost updates are impossible** — every write carries an expected `Version`;
  concurrent mutations surface as `409 Conflict` instead of overwriting.
- **Real authorization** — `admin`, `approver`, `requester`, and `viewer`, with
  department-scoped approvals and no self-approval for non-admins.
- **Password login with cookie sessions** — `POST /api/v1/auth/login` returns the
  role-bearing JWT as a `Secure`, `HttpOnly`, `SameSite=Lax` cookie; bearer API
  keys still work for service-to-service calls.
- **Auditable by construction** — each transition writes an immutable audit row
  in the same transaction; a database trigger rejects `UPDATE` and `DELETE`.
- **Retry-safe by default** — `Idempotency-Key` replay scoped per actor, RFC 9457
  problem documents, and a strict 1 MiB JSON decoder.
- **Backpressure built in** — token-bucket rate limiting per caller, shared
  across replicas through Redis, with anonymous traffic throttled too.
- **Async side effects** — a transactional outbox plus RabbitMQ consumers for
  notifications and ERP sync, so HTTP latency never depends on a webhook.
- **Boring operations** — liveness/readiness with a degraded state, Prometheus
  metrics, graceful shutdown, embedded reversible migrations.

## Table of contents

- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Walkthrough](#walkthrough)
- [Running locally](#running-locally)
- [API](#api)
- [Authentication and roles](#authentication-and-roles)
- [Configuration](#configuration)
- [Reliability and delivery semantics](#reliability-and-delivery-semantics)
- [Operations](#operations)
- [Development](#development)
- [Project layout](#project-layout)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

## Architecture

```
cmd/api · cmd/worker · cmd/migrate
        │
        ▼
transport/httpapi ─────── router, middleware, RFC 9457 responses
        │
        ▼
application ───────────── use cases, authorization, cache, event emission
        │
        ▼
domain ────────────────── aggregate, state machine, repository ports
        ▲
        │
infrastructure ────────── memory · postgres · redis · rabbitmq
```

Dependencies point inward. The domain layer imports only the standard library,
so business rules run without a database; the application layer depends on
interfaces, never on an adapter; infrastructure implements those interfaces.
The same repository contract suite runs against the in-memory and PostgreSQL
adapters, which is why the fast tests are meaningful.

A full tour — aggregates, transaction boundaries, outbox relay, failure modes —
is in [`docs/architecture.md`](docs/architecture.md).

## Quick start

The fastest path runs the complete stack (API, PostgreSQL, Redis, RabbitMQ, and
the worker) in containers:

```bash
git clone https://github.com/amirfaisalz/procure-flow.git
cd procure-flow
docker compose -f deployments/docker-compose.yml up --build
```

| Service | Address |
| --- | --- |
| API | <http://localhost:8080> |
| Interactive API reference | <http://localhost:8080/docs> |
| RabbitMQ management | <http://localhost:15672> (`guest` / `guest`) |
| PostgreSQL | `127.0.0.1:55432` (`procure` / `procure`) |
| Redis | `127.0.0.1:56379` |

Compose turns authentication on, seeds four login accounts, and keeps two
**local-only** bearer tokens for scripts: an admin
(`local-admin-token-0123456789`) and an Engineering approver
(`local-approver-token-0123456789`). Replace all of them before exposing the
stack to anything.

| Demo account | Role | Department |
| --- | --- | --- |
| `admin@procure.local` | `admin` | — |
| `approver@procure.local` | `approver` | Engineering |
| `requester@procure.local` | `requester` | Engineering |
| `viewer@procure.local` | `viewer` | — |

Every demo account uses the password `demo-password-123` unless the seed service
was given `SEED_PASSWORD`.

## Walkthrough

The lifecycle is deliberately small:

```
draft ──submit──▶ submitted ──approve──▶ approved
  │                   │
  │                   └──reject──▶ rejected
  └──cancel───────────┴──cancel──▶ cancelled
```

Log in as the seeded requester. The JWT lands in a cookie jar, so it never
reaches JavaScript:

```bash
export API=http://localhost:8080

curl -s -c requester.txt -X POST "$API/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"requester@procure.local","password":"demo-password-123"}'
```

Create a draft, submit it, then read it back:

```bash
ID=$(curl -s -b requester.txt -X POST "$API/api/v1/procurements" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-create-1' \
  -d '{
    "title": "Laptop procurement for engineering",
    "description": "20 development laptops",
    "department": "Engineering",
    "requester": "Amir",
    "amount": 250000000,
    "currency": "IDR"
  }' | jq -r '.data.id')

curl -s -b requester.txt -X POST "$API/api/v1/procurements/$ID/submit"
curl -s -b requester.txt "$API/api/v1/procurements/$ID" | jq
```

The requester cannot approve their own request, so log in as the approver:

```bash
curl -s -c approver.txt -X POST "$API/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"approver@procure.local","password":"demo-password-123"}'

curl -s -b approver.txt -X POST "$API/api/v1/procurements/$ID/approve"
curl -s -b approver.txt "$API/api/v1/procurements/$ID/audit" | jq
```

Without `jq`, copy `data.id` from the create response. Lists, filters, and the
audit trail use the same cookie:

```bash
curl -s -b approver.txt "$API/api/v1/procurements?status=approved&page=1&page_size=20"
curl -s -b approver.txt "$API/api/v1/auth/me"
```

The audit trail is the interesting part: every state change is recorded newest
first, with the actor, the transition, and the resulting version.

Prefer clicking to curling? Open <http://localhost:8080/docs> for a Scalar
reference with an in-browser request client.

## Running locally

No containers needed for the in-memory mode:

```bash
make run
```

Data lives in memory and disappears with the process. Authentication is on by
default: the API generates an ephemeral signing key and seeds four demo accounts
in memory, so log in with `admin@procure.local` / `demo-password-123` before
calling `/api/v1`:

```bash
curl -s -c cookies.txt -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@procure.local","password":"demo-password-123"}'
```

`APP_ENV=production` refuses to start without `DATABASE_URL`, so a misconfigured
deploy fails loudly instead of silently losing writes.

For the full stack on the host:

```bash
make deps-up            # PostgreSQL :55432 · Redis :56379 · RabbitMQ :5672

export DATABASE_URL='postgres://procure:procure@127.0.0.1:55432/procure?sslmode=disable'
export REDIS_URL='redis://127.0.0.1:56379/0'
export RABBITMQ_URL='amqp://guest:guest@127.0.0.1:5672/'
export RATE_LIMIT_STORE=redis

make run                # applies embedded migrations on startup
make worker             # in a second terminal
```

The worker logs one notification per lifecycle event and simulates the ERP sync
unless webhooks are configured. Migrations can also run as a separate step:

```bash
make migrate-up     DATABASE_URL='postgres://...'
make migrate-status DATABASE_URL='postgres://...'
go run ./cmd/migrate down
```

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness; `200` with `degraded` when an optional dependency is down |
| `GET` | `/metrics` | Prometheus text format |
| `GET` | `/docs` | Scalar API reference |
| `GET` | `/openapi.yaml` | OpenAPI 3.1 contract |
| `POST` | `/api/v1/auth/login` | Email + password login; sets the session cookie |
| `POST` | `/api/v1/auth/logout` | Clears the session cookie |
| `GET` | `/api/v1/auth/me` | Current account, roles, and department |
| `POST` | `/api/v1/procurements` | Create a draft; accepts `Idempotency-Key` |
| `GET` | `/api/v1/procurements` | List and filter (`q`, `status`, `page`, `page_size`) |
| `GET` | `/api/v1/procurements/{id}` | Read one aggregate (cached) |
| `GET` | `/api/v1/procurements/{id}/audit` | Audit trail, newest first |
| `POST` | `/api/v1/procurements/{id}/submit` | `draft → submitted` |
| `POST` | `/api/v1/procurements/{id}/approve` | `submitted → approved` |
| `POST` | `/api/v1/procurements/{id}/reject` | `submitted → rejected`; requires a reason (3–500 chars) |
| `POST` | `/api/v1/procurements/{id}/cancel` | `draft` or `submitted → cancelled` |

Successful responses use a small envelope:

```json
{ "data": { }, "meta": { } }
```

Lists add `meta.page`, `meta.page_size`, `meta.total`, and `meta.total_pages`;
`page_size` is capped at 100. Errors are RFC 9457 problem documents:

```json
{
  "type": "about:blank",
  "title": "Validation failed",
  "status": 422,
  "detail": "one or more fields are invalid",
  "instance": "/api/v1/procurements",
  "code": "VALIDATION_ERROR",
  "request_id": "01J…",
  "errors": { "title": "must be between 3 and 200 characters" }
}
```

The whole contract lives in [`api/openapi.yaml`](api/openapi.yaml). It also
imports into Bruno, Postman, Insomnia, Hoppscotch, or `openapi-generator`.
[`api/requests.http`](api/requests.http) has the happy paths, the full workflow,
and negative cases for VS Code REST Client or JetBrains HTTP Client.

## Authentication and roles

`/healthz`, `/readyz`, `/metrics`, `/docs`, `/openapi.yaml`, and the login and
logout endpoints are public. Every other `/api/v1` route needs either a session
cookie or a bearer credential.

### Session login

`POST /api/v1/auth/login` exchanges an email and password for the same HS256 JWT
the bearer authenticator verifies, delivered as a cookie:

```bash
curl -s -c cookies.txt -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@procure.local","password":"demo-password-123"}'
```

The cookie is `HttpOnly`, `SameSite=Lax`, scoped to `/api/v1`, and `Secure`
outside development. The response body carries the account and expiry only —
never the token. `GET /api/v1/auth/me` echoes the verified claims, and
`POST /api/v1/auth/logout` clears the cookie.

Accounts live in the `users` table with bcrypt hashes. `make seed` (or the
compose `seed` service) creates four of them:

| Email | Role | Department |
| --- | --- | --- |
| `admin@procure.local` | `admin` | — |
| `approver@procure.local` | `approver` | Engineering |
| `requester@procure.local` | `requester` | Engineering |
| `viewer@procure.local` | `viewer` | — |

The password is `SEED_PASSWORD`, or `demo-password-123` outside production.
Because the session is a cookie, state-changing requests also rely on
`SameSite=Lax` plus the required `application/json` content type, which a
cross-site form cannot send. Logout removes the cookie but does not revoke the
stateless JWT, so a copied token stays valid until it expires.

### Bearer credentials

Static API keys and externally issued HS256 JWTs keep working for scripts and
service-to-service calls. JWTs must carry `sub`, `exp`, a valid `roles` claim,
and match the configured issuer and audience:

```bash
curl -s http://localhost:8080/api/v1/procurements \
  -H 'Authorization: Bearer local-admin-token-0123456789'
```

When both a cookie and an `Authorization` header are present, the header wins.

| Role | Permissions |
| --- | --- |
| `admin` | Everything, including cross-department approval and ownership overrides |
| `approver` | Approve or reject submitted requests in their own department, never their own |
| `requester` | Create requests; submit and cancel the ones they own |
| `viewer` | Read requests and audit trails |

Ownership is recorded as `requester_id`, derived from the token subject rather
than the request body. Non-admin approvers cannot approve their own requests;
admins may, as an explicit override. Authorization fails closed: a missing actor
is forbidden, and a missing authenticator rejects every API call. Login is
required in every environment; only development accepts `AUTH_ENABLED=false`,
which injects a local admin actor, while staging and production refuse to start
without authentication.

### Idempotency

`POST /api/v1/procurements` accepts an `Idempotency-Key`. Keys are scoped per
actor and retained for `IDEMPOTENCY_TTL` (24h by default):

- same key and body → replays the original status, body, and `Location`, with
  `Idempotent-Replayed: true`;
- same key, different body → `422 IDEMPOTENCY_KEY_REUSED`;
- retry while the first request is still running → `409 IDEMPOTENCY_IN_PROGRESS`
  with `Retry-After: 1`.

`5xx` responses release the claim so the client can retry, and request bodies
are capped at 1 MiB (`413` beyond that).

### Rate limiting

Authenticated requests consume a bucket keyed by subject; failed authentication
consumes the remote IP's bucket **before** the `401` is written, so credential
guessing is throttled as well. Responses carry `X-RateLimit-Limit` and
`X-RateLimit-Remaining`; an exhausted bucket returns `429 RATE_LIMITED` with
`Retry-After`.

With `RATE_LIMIT_STORE=redis` the bucket lives in Redis behind an atomic Lua
script, so every replica shares one limit. If Redis fails the middleware falls
back to a per-process bucket; if both fail, requests are allowed and the failure
is logged — availability wins over strictness here, deliberately.

## Configuration

Environment variables only; `.env` is not read automatically. The essentials:

| Variable | Default | Notes |
| --- | --- | --- |
| `APP_ENV` | `development` | `production` turns on strict preconditions |
| `HTTP_ADDR` | `:8080` | Listen address |
| `DATABASE_URL` | *(empty)* | Empty selects the in-memory repository |
| `AUTH_ENABLED` | `true` | Every `/api/v1` route needs a login; development may opt out |
| `AUTH_API_KEYS` / `AUTH_JWT_SECRET` | *(empty)* | Credentials; development generates a temporary secret |
| `AUTH_SESSION_TTL` | `8h` | Login cookie lifetime |
| `AUTH_COOKIE_SECURE` | `true` outside development | Keep `true` behind TLS |
| `SEED_PASSWORD` | demo default outside production | Password for seeded accounts |
| `CORS_ALLOWED_ORIGINS` | *(empty)* | Exact origins only; wildcards rejected |
| `REDIS_URL` | *(empty)* | Enables shared rate limiting and the read cache |
| `RABBITMQ_URL` | *(empty)* | Enables the outbox relay and lifecycle events |
| `METRICS_ENABLED` | `true` | Prometheus endpoint |
| `DOCS_ENABLED` | `true` outside production | `/docs` and `/openapi.yaml` |

Every key, its default, and the validation rules are documented in
[`docs/configuration.md`](docs/configuration.md) and
[`.env.example`](.env.example).

## Reliability and delivery semantics

**Reads** — `GET /api/v1/procurements/{id}` is cache-aside. A miss loads the
aggregate, stores it as JSON in Redis for `CACHE_TTL`, and every write
invalidates the key after commit. Each entry carries a generation marker: a read
that observed the old generation cannot repopulate a stale value, and cache
errors are treated as misses, never as failures. Keys are versioned
(`procurement:v1:<id>`); lists are not cached.

**Writes** — aggregate changes, audit rows, and outbox rows share one
transaction. A failed audit write rolls the change back. Under contention the
optimistic version check decides the winner and the loser gets `409`.

**Events** — when `RABBITMQ_URL` is set, a relay leases due outbox rows with
`FOR UPDATE SKIP LOCKED`, publishes to a durable topic exchange in confirm mode,
and marks them published. Failures back off from 1s to 5m. A crash between
publish and mark produces a duplicate, so delivery is **at-least-once** — the
`event_id` in every payload is the deduplication handle for consumers.

**Consumers** — `cmd/worker` declares two durable queues. `procure.notifications`
binds every lifecycle key and notifies the requester; `procure.integrations`
binds `procurement.approved` and syncs with the ERP. Handlers retry three times
with exponential backoff, then dead-letter to `<queue>.dlq` through
`<queue>.dlx`. Routing keys are the event types (`procurement.created`,
`submitted`, `approved`, `rejected`, `cancelled`).

The envelope is `{id, type, aggregate_type, aggregate_id, actor_id,
occurred_at, payload}`; the payload carries a procurement snapshot (`number`,
`title`, `department`, `requester`, `amount`, `currency`, `status`,
`from_status`, `version`, `actor`).

## Operations

### Health

`/healthz` answers liveness. `/readyz` gates on required dependencies only —
today PostgreSQL:

```json
{ "status": "degraded", "checks": { "repository": "ok", "redis": "unavailable" } }
```

A Redis or RabbitMQ outage returns `200 degraded` and the instance keeps taking
traffic, because both features have local fallbacks. Only a failing required
dependency returns `503 not_ready`.

### Metrics and tracing

- `http_requests_total{method,route,status}`
- `http_request_duration_seconds{method,route,status}`

The `route` label is the `ServeMux` pattern, not the raw path, so identifiers
never inflate cardinality. `/metrics` is public like the probes — restrict it at
the network edge.

Every response carries `X-Request-ID`: a valid incoming value is preserved,
otherwise one is generated, and the access log correlates on it. Requests accept
and emit W3C `traceparent`; `X-Trace-ID` and the access logs carry `trace_id` and
`span_id`. Exporting spans to an OpenTelemetry backend is on the
[roadmap](#roadmap).

### Shutdown

The API stops accepting connections on `SIGINT`/`SIGTERM`, drains in-flight
requests within `HTTP_SHUTDOWN_TIMEOUT`, and joins the outbox relay before
closing the publisher, Redis client, and pool. The worker stops consuming and
leaves in-flight deliveries unsettled so the broker can requeue them.

### Container

```bash
make docker-build
docker run --rm -p 8080:8080 \
  -e APP_ENV=production \
  -e DATABASE_URL='postgres://user:pass@host:5432/procure?sslmode=disable' \
  -e AUTH_ENABLED=true \
  -e AUTH_API_KEYS='[{"token":"replace-me-0123456789","subject":"user_admin","name":"Admin","roles":["admin"]}]' \
  procure-flow:local
```

The image ships four binaries: `/api` (default entrypoint), `/worker`,
`/migrate` for deployment jobs, and `/seed` for demo or bootstrap accounts.
Compose runs it read-only with `no-new-privileges` and a 30s grace period.

## Development

Go 1.26 or newer. Docker is optional, but the integration tests need the
compose services.

```bash
make help              # every target, documented
make test              # unit tests; integration tests skip without services
make test-race         # same tests with the race detector
make test-integration  # race tests against PostgreSQL, Redis, and RabbitMQ
make check             # go fmt + vet + integration tests
make cover             # coverage summary
```

| Target | What it does |
| --- | --- |
| `make run` | Run the API |
| `make worker` | Run the async worker |
| `make deps-up` / `make db-up` | Start dependencies |
| `make db-reset` | Destroy and recreate the database volume |
| `make migrate-up` / `make migrate-status` | Migration CLI wrappers |
| `make seed` | Create the demo login accounts (`DATABASE_URL=…`) |
| `make smoke` | Smoke-test a running instance (`BASE_URL=…`) |
| `make build` | Binary in `bin/` |

Tests live next to the code they exercise. The shared repository contract in
`internal/infrastructure/repositorytest` runs against both adapters, so the
in-memory implementation and PostgreSQL cannot drift apart silently. Integration
tests are gated by `TEST_DATABASE_URL`, `TEST_REDIS_URL`, and
`TEST_RABBITMQ_URL`.

CI runs `gofmt`, `go mod tidy -diff`, `go vet`, `go test -race` with PostgreSQL,
Redis, and RabbitMQ services, a build, and a container build.

## Project layout

```
cmd/api/                       HTTP entrypoint and outbox relay
cmd/worker/                    async notification and ERP consumers
cmd/migrate/                   migration CLI (up, down, status)
cmd/seed/                      demo account seeder
internal/config/               environment configuration
internal/domain/               domain errors, procurement aggregate, ports
internal/application/          use cases, cache port, event emission
internal/infrastructure/       memory · postgres · redis · rabbitmq adapters
internal/platform/             IDs, logging, auth, rate limiting, metrics,
                               tracing, outbox relay
internal/transport/httpapi/    router, handlers, middleware, RFC 9457 helpers
api/                           OpenAPI contract and HTTP request collection
configs/                       example configuration
deployments/                   compose stack
docs/                          architecture and configuration references
migrations/                    embedded, reversible SQL migrations
scripts/                       operational scripts
```

## Roadmap

- OIDC discovery and key rotation for JWT verification.
- OpenTelemetry span export (propagation already exists).
- Kubernetes manifests, dead-letter replay tooling, and backup/retention policy.
- Load and fuzz tests around the state machine and the middleware chain.

## Contributing

Issues and pull requests are welcome. For a change to land smoothly:

1. Fork the repository and branch from `main`.
2. Keep `gofmt`, `go vet`, and `go test -race ./...` green; `make check` runs the
   full set against the compose services.
3. Add a test for every behaviour change. Bug fixes should start with one that
   fails.
4. Keep pull requests focused — one concern per PR, with a short explanation of
   the tradeoff rather than a changelog.

Found a security problem? Please use GitHub's private vulnerability reporting
instead of opening a public issue.

## License

Released under the [MIT License](LICENSE). Use it, fork it, ship it.
