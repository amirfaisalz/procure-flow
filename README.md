# procure-flow

Enterprise procurement workflow API built on the Go standard library
(`net/http`, Go 1.22+ routing, `log/slog`, `database/sql`). There is no
third-party web framework; PostgreSQL is reached through the pure-Go `pgx`
`database/sql` driver.

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
- Scalar API reference (OpenAPI 3.1) and an HTTP request collection.
- CI that runs `gofmt`, `go vet`, race tests against PostgreSQL, and a build.

## Requirements

- Go 1.26+
- Docker (optional, for the local PostgreSQL and container image)

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
| `DOCS_ENABLED` | `true` (non-production) | Serve `/docs` and `/openapi.yaml` |

`HTTP_REQUEST_TIMEOUT` must not exceed `HTTP_WRITE_TIMEOUT`. Wildcard CORS
origins are rejected. `DB_MAX_IDLE_CONNS` must not exceed `DB_MAX_OPEN_CONNS`.

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
instead of application startup.

## Development

```bash
make test              # go test ./... (integration tests skip without a DB)
make test-race         # go test -race ./...
make test-integration  # race tests against the local PostgreSQL
make db-up             # start PostgreSQL
make db-reset          # destroy the volume and recreate
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
cmd/migrate/                   migration CLI (up, down, status)
internal/config/               environment configuration
internal/domain/               shared domain errors
internal/domain/procurement/   aggregate, state machine, repository port
internal/application/          use-case services
internal/infrastructure/       memory + PostgreSQL adapters, repository contract tests
internal/platform/             request IDs, ID generator, logging
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
  procure-flow:local```

Or run the full stack:

```bash
docker compose -f deployments/docker-compose.yml up --build
```

## Roadmap

- Authentication/authorization (JWT/OIDC, roles) and per-actor audit trail.
- OpenTelemetry tracing and Prometheus metrics.
- Rate limiting and idempotency keys.
- Kubernetes manifests and load/fuzz tests.
