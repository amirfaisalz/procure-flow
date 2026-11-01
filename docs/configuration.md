# Configuration

`procure-flow` is configured exclusively through environment variables. `.env`
files are **not** loaded automatically — load them in your shell or pass them to
your process manager.

```bash
cp .env.example .env
set -a && . ./.env && set +a
make run
```

[`../.env.example`](../.env.example) and
[`../configs/config.example.env`](../configs/config.example.env) mirror the
tables below with inline comments.

## Application

| Variable | Default | Description |
| --- | --- | --- |
| `APP_ENV` | `development` | `development`, `staging`, or `production` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |

## HTTP server

| Variable | Default | Description |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Listen address |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Header read timeout |
| `HTTP_READ_TIMEOUT` | `15s` | Full request read timeout |
| `HTTP_WRITE_TIMEOUT` | `30s` | Response write timeout |
| `HTTP_IDLE_TIMEOUT` | `60s` | Keep-alive idle timeout |
| `HTTP_REQUEST_TIMEOUT` | `20s` | Per-request context deadline |
| `HTTP_SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown budget |
| `HTTP_MAX_HEADER_BYTES` | `1048576` | Maximum request header size |

## Database

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | *(empty)* | PostgreSQL URL; empty selects the in-memory repository |
| `DB_MAX_OPEN_CONNS` | `25` | Maximum open connections |
| `DB_MAX_IDLE_CONNS` | `25` | Maximum idle connections |
| `DB_CONN_MAX_LIFETIME` | `30m` | Connection maximum lifetime |
| `DB_CONN_MAX_IDLE_TIME` | `5m` | Connection maximum idle time |
| `DB_AUTO_MIGRATE` | `true` | Apply embedded migrations on startup |

## CORS

| Variable | Default | Description |
| --- | --- | --- |
| `CORS_ALLOWED_ORIGINS` | *(empty)* | Comma-separated exact origins; empty disables CORS |

## Authentication

| Variable | Default | Description |
| --- | --- | --- |
| `AUTH_ENABLED` | `true` in staging/production, `false` in development | Require authentication |
| `AUTH_API_KEYS` | *(empty)* | JSON array of static API keys |
| `AUTH_JWT_SECRET` | *(empty)* | HS256 secret, at least 32 characters |
| `AUTH_JWT_ISSUER` | `procure-flow` | Required JWT `iss` |
| `AUTH_JWT_AUDIENCE` | `procure-flow-api` | Required JWT `aud` |
| `AUTH_JWT_LEEWAY` | `30s` | Clock-skew tolerance |

## Idempotency

| Variable | Default | Description |
| --- | --- | --- |
| `IDEMPOTENCY_ENABLED` | `true` | Replay protection for `POST /api/v1/procurements` |
| `IDEMPOTENCY_TTL` | `24h` | How long stored responses remain replayable |

## Rate limiting

| Variable | Default | Description |
| --- | --- | --- |
| `RATE_LIMIT_ENABLED` | `true` in staging/production, `false` in development | Enable per-caller limiting |
| `RATE_LIMIT_RPS` | `20` | Sustained requests per second per caller |
| `RATE_LIMIT_BURST` | `40` | Burst allowance per caller |
| `RATE_LIMIT_STORE` | `auto` | `auto`, `memory`, or `redis`; `redis` requires `REDIS_URL` |

## Redis

| Variable | Default | Description |
| --- | --- | --- |
| `REDIS_URL` | *(empty)* | `redis://` or `rediss://` URL; enables shared state |
| `REDIS_DIAL_TIMEOUT` | `2s` | Redis connect and pool timeout |
| `REDIS_COMMAND_TIMEOUT` | `1s` | Redis read/write command timeout |
| `CACHE_ENABLED` | `true` when `REDIS_URL` is set | Read-through cache for `GET` by id |
| `CACHE_TTL` | `30s` | Maximum staleness bound for cached aggregates |

## RabbitMQ and the worker

| Variable | Default | Description |
| --- | --- | --- |
| `RABBITMQ_URL` | *(empty)* | `amqp://` or `amqps://` URL; enables async events |
| `RABBITMQ_EXCHANGE` | `procure.events` | Durable topic exchange for lifecycle events |
| `NOTIFICATION_WEBHOOK_URL` | *(empty)* | Worker POSTs requester notifications here |
| `ERP_WEBHOOK_URL` | *(empty)* | Worker POSTs approved procurements here |

The worker reads `LOG_LEVEL`, `RABBITMQ_*`, and the two webhook URLs. Without a
webhook the worker logs the action instead, so the pipeline stays runnable
locally.

## Observability and documentation

| Variable | Default | Description |
| --- | --- | --- |
| `METRICS_ENABLED` | `true` | Serve Prometheus metrics at `/metrics` |
| `DOCS_ENABLED` | `true` outside production | Serve `/docs` and `/openapi.yaml` |

## Validation

The process refuses to start when a value is malformed or a production
precondition is missing:

- `APP_ENV` must be `development`, `staging`, or `production`.
- `HTTP_REQUEST_TIMEOUT` must not exceed `HTTP_WRITE_TIMEOUT`.
- `DB_MAX_IDLE_CONNS` must not exceed `DB_MAX_OPEN_CONNS`.
- `RATE_LIMIT_RPS` and `RATE_LIMIT_BURST` must be finite, positive numbers;
  `NaN` and `Inf` are rejected rather than silently disabling the limiter.
- CORS origins must be exact `scheme://host[:port]` values; wildcards are
  rejected.
- `RATE_LIMIT_STORE=redis` requires `REDIS_URL`, and `REDIS_URL` must use the
  `redis://` or `rediss://` scheme.
- Webhook URLs must use `http://` or `https://`.
- Production requires `DATABASE_URL`, `AUTH_ENABLED=true`, and at least one of
  `AUTH_API_KEYS` or `AUTH_JWT_SECRET`.

## Profiles

### In-memory (zero dependencies)

```bash
make run
```

`DATABASE_URL` is empty, so the API uses the in-memory repository. All state is
lost on exit. Authentication and rate limiting default to off in `development`.

### Full local stack

```bash
make deps-up

export DATABASE_URL='postgres://procure:procure@127.0.0.1:55432/procure?sslmode=disable'
export REDIS_URL='redis://127.0.0.1:56379/0'
export RABBITMQ_URL='amqp://guest:guest@127.0.0.1:5672/'
export RATE_LIMIT_STORE=redis
export AUTH_ENABLED=true
export AUTH_API_KEYS='[{"token":"local-admin-token-0123456789","subject":"user_admin","name":"Local Admin","roles":["admin"]}]'

make run
make worker   # second terminal
```

This enables PostgreSQL persistence, shared rate limiting, the read cache, and
the outbox relay plus worker.

### Production

```bash
export APP_ENV=production
export DATABASE_URL='postgres://user:pass@db:5432/procure?sslmode=require'
export AUTH_ENABLED=true
export AUTH_JWT_SECRET='at-least-32-characters-long-secret'
export AUTH_JWT_ISSUER='your-issuer'
export AUTH_JWT_AUDIENCE='your-audience'
export CORS_ALLOWED_ORIGINS='https://app.example.com'
export DOCS_ENABLED=false
```

Set `DB_AUTO_MIGRATE=false` when migrations are applied by a deployment step
(the image ships `/migrate` for exactly that). Put a reverse proxy or ingress in
front of `/metrics`, which is public like the probes.
