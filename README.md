# procure-flow

Enterprise procurement workflow API built with the Go standard library only
(`net/http`, Go 1.22+ routing, `log/slog`). No third-party runtime dependencies.

## Features

- Layered architecture: domain → application → infrastructure/transport.
- Procurement aggregate with an explicit status state machine.
- Optimistic concurrency via a monotonically increasing `Version`.
- RFC 9457 `application/problem+json` error responses.
- Strict JSON decoding: 1 MiB limit, unknown fields rejected, `application/json` required.
- Request correlation (`X-Request-ID`), structured access logs, panic recovery.
- Security headers, exact-origin CORS, per-request timeout.
- Liveness/readiness probes with pluggable dependency checks.
- Graceful shutdown on `SIGINT`/`SIGTERM`.
- In-memory repository adapter for local development and tests.

## Requirements

- Go 1.26+

## Quick start

```bash
make run
```

The API listens on `:8080` by default.

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
```

```bash
curl -s http://localhost:8080/api/v1/procurements
curl -s -X POST http://localhost:8080/api/v1/procurements/<id>/submit
curl -s -X POST http://localhost:8080/api/v1/procurements/<id>/approve
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
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
| `CORS_ALLOWED_ORIGINS` | *(empty)* | Comma-separated exact origins |
| `DOCS_ENABLED` | `true` (non-production) | Serve `/docs` and `/openapi.yaml` |

`HTTP_REQUEST_TIMEOUT` must not exceed `HTTP_WRITE_TIMEOUT`. Wildcard CORS
origins are rejected.

## Development

```bash
make test        # go test ./...
make test-race   # go test -race ./...
make cover       # coverage report
make vet         # go vet ./...
make fmt         # gofmt -w
make build       # binary in bin/api
```

Tests are written first (TDD) and live next to the code they exercise.

## Project layout

```
cmd/api/                     application entrypoint
internal/config/             environment configuration
internal/domain/             shared domain errors
internal/domain/procurement/ aggregate, state machine, repository port
internal/application/        use-case services
internal/infrastructure/     repository adapters (in-memory today)
internal/platform/           request IDs, ID generator, logging
internal/transport/httpapi/  router, handlers, middleware, response helpers
api/                         OpenAPI contract
configs/                     example configuration
deployments/                 container/deployment assets
docs/                        architecture notes
migrations/                  database migration placeholder
scripts/                     operational scripts
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
| Go tests | `make test-race` — end-to-end `httptest` coverage of every route |
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
docker run --rm -p 8080:8080 -e APP_ENV=production procure-flow:local
```

## Roadmap

- SQL repository adapter and migrations.
- Authentication/authorization and per-actor audit trail.
- OpenTelemetry tracing and metrics.
