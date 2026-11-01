# Configuration

See also: [user journeys](user-journey.md), [architecture](architecture.md), and
[technology](technology.md).

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
| `DATABASE_URL` | *(required)* | PostgreSQL connection URL; no other backend exists |
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
| `AUTH_ENABLED` | `true` | Require a login for every `/api/v1` route |
| `AUTH_API_KEYS` | *(empty)* | JSON array of static API keys |
| `AUTH_JWT_SECRET` | *(generated in development)* | HS256 secret, at least 32 characters |
| `AUTH_JWT_ISSUER` | `procure-flow` | Required JWT `iss` |
| `AUTH_JWT_AUDIENCE` | `procure-flow-api` | Required JWT `aud` |
| `AUTH_JWT_LEEWAY` | `30s` | Clock-skew tolerance |
| `AUTH_SESSION_TTL` | `8h` | Login session lifetime (1m–720h) |
| `AUTH_COOKIE_NAME` | `procure_session` | Session cookie name |
| `AUTH_COOKIE_SECURE` | `true` outside development | `Secure` attribute on the session cookie |

Setting `AUTH_JWT_SECRET` enables `POST /api/v1/auth/login`, which signs the JWT
with the same key the bearer authenticator verifies. Without it, login and
logout are not registered and only API keys work; `GET /api/v1/auth/me` still
describes the caller.

## Seeding

`cmd/seed` (or `make seed`) writes four demo accounts to PostgreSQL and is
idempotent: the email is the conflict key, so re-running rotates the password
instead of duplicating rows. It applies pending migrations first.

| Variable | Default | Description |
| --- | --- | --- |
| `SEED_PASSWORD` | `demo-password-123` outside production | Password for every demo account (minimum 8 characters) |

`SEED_PASSWORD` is required when `APP_ENV=production`. The accounts are
`admin@procure.local` (admin), `approver@procure.local` (approver, Engineering),
`requester@procure.local` (requester, Engineering), and `viewer@procure.local`
(viewer). Passwords are stored as bcrypt hashes; plaintext never reaches the
database.

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
- `AUTH_SESSION_TTL` must be between 1m and 720h, and `AUTH_COOKIE_NAME` may
  only contain letters, digits, dashes, and underscores.
- `RATE_LIMIT_STORE=redis` requires `REDIS_URL`, and `REDIS_URL` must use the
  `redis://` or `rediss://` scheme.
- Webhook URLs must use `http://` or `https://`.
- `DATABASE_URL` is required in every environment.
- Staging and production require `AUTH_ENABLED=true` and at least one of
  `AUTH_API_KEYS` or `AUTH_JWT_SECRET`.
- In development, an unset `AUTH_JWT_SECRET` is replaced by a random ephemeral
  secret so login works out of the box. Sessions do not survive a restart.

## Profiles

### Local

```bash
make deps-up

export DATABASE_URL='postgres://procure:procure@127.0.0.1:55432/procure?sslmode=disable'
export REDIS_URL='redis://127.0.0.1:56379/0'
export RABBITMQ_URL='amqp://guest:guest@127.0.0.1:5672/'
export RATE_LIMIT_STORE=redis

make run       # applies migrations on startup
make seed      # demo login accounts
make worker    # second terminal
```

Redis and RabbitMQ are optional: leave `REDIS_URL` and `RABBITMQ_URL` unset and
the API runs against PostgreSQL alone, with an in-process rate limiter and no
event emission. Authentication stays on; in development an unset
`AUTH_JWT_SECRET` is replaced by an ephemeral key, and `make seed` creates the
four demo accounts so you can log in with `admin@procure.local` /
`demo-password-123`.

### Production

```bash
export APP_ENV=production
export DATABASE_URL='postgres://user:pass@db:5432/procure?sslmode=require'
export AUTH_ENABLED=true
export AUTH_JWT_SECRET='at-least-32-characters-long-secret'
export AUTH_JWT_ISSUER='your-issuer'
export AUTH_JWT_AUDIENCE='your-audience'
export AUTH_COOKIE_SECURE=true
export CORS_ALLOWED_ORIGINS='https://app.example.com'
export DOCS_ENABLED=false
```

Set `DB_AUTO_MIGRATE=false` when migrations are applied by a deployment step
(the image ships `/migrate` for exactly that). Seed accounts from a deployment
job with `/seed` and an explicit `SEED_PASSWORD`, or manage accounts in your own
identity system and keep using bearer tokens. Put a reverse proxy or ingress in
front of `/metrics`, which is public like the probes.
