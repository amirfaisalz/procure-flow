#!/usr/bin/env bash
# Smoke-test a running procure-flow instance.
#
# Usage:
#   scripts/smoke.sh [base-url]
#
# Environment:
#   AUTH_TOKEN  bearer token sent to /api/v1 when authentication is enabled
#               (for example `local-admin-token-0123456789` from compose).
#
# Requires: curl, jq.

set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"
IDEMPOTENCY_KEY="smoke-create-$(date +%s)-$$"

AUTH=()
if [ -n "${AUTH_TOKEN:-}" ]; then
  AUTH=(-H "Authorization: Bearer $AUTH_TOKEN")
fi

echo "==> health"
curl -fsS "$BASE_URL/healthz" | jq .
curl -fsS "$BASE_URL/readyz" | jq .

echo "==> create (with Idempotency-Key)"
CREATE_BODY='{
  "title": "Laptop procurement for engineering",
  "description": "20 units of development laptops",
  "department": "Engineering",
  "requester": "Amir",
  "amount": 250000000,
  "currency": "IDR"
}'

CREATED="$(curl -fsS -X POST "$BASE_URL/api/v1/procurements" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $IDEMPOTENCY_KEY" \
  ${AUTH[@]+"${AUTH[@]}"} \
  -d "$CREATE_BODY")"
echo "$CREATED" | jq .

ID="$(echo "$CREATED" | jq -r '.data.id')"
if [ -z "$ID" ] || [ "$ID" = "null" ]; then
  echo "create response did not include data.id" >&2
  exit 1
fi

echo "==> replay the same request"
REPLAY_HEADERS="$(mktemp)"
REPLAYED="$(curl -fsS -D "$REPLAY_HEADERS" -X POST "$BASE_URL/api/v1/procurements" \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $IDEMPOTENCY_KEY" \
  ${AUTH[@]+"${AUTH[@]}"} \
  -d "$CREATE_BODY")"
if ! grep -qi '^Idempotent-Replayed: true' "$REPLAY_HEADERS"; then
  echo "replay did not return Idempotent-Replayed: true" >&2
  exit 1
fi
REPLAYED_ID="$(echo "$REPLAYED" | jq -r '.data.id')"
if [ "$REPLAYED_ID" != "$ID" ]; then
  echo "replay returned id $REPLAYED_ID, want $ID" >&2
  exit 1
fi
rm -f "$REPLAY_HEADERS"
echo "replayed $ID"

echo "==> submit"
curl -fsS -X POST "$BASE_URL/api/v1/procurements/$ID/submit" \
  ${AUTH[@]+"${AUTH[@]}"} | jq '.data.status'

echo "==> approve"
curl -fsS -X POST "$BASE_URL/api/v1/procurements/$ID/approve" \
  ${AUTH[@]+"${AUTH[@]}"} | jq '.data.status'

echo "==> list"
curl -fsS "$BASE_URL/api/v1/procurements?page=1&page_size=10" \
  ${AUTH[@]+"${AUTH[@]}"} | jq '.meta'

echo "==> audit (paginated)"
curl -fsS "$BASE_URL/api/v1/procurements/$ID/audit?page=1&page_size=2" \
  ${AUTH[@]+"${AUTH[@]}"} | jq '{actions: [.data[].action], meta: .meta}'

echo "==> metrics"
if curl -fsS "$BASE_URL/metrics" | grep -m 3 -E '^http_requests_total\{'; then
  :
else
  echo "metrics endpoint unavailable or empty (METRICS_ENABLED may be false)"
fi

echo "==> 404 problem document"
curl -sS -o /dev/null -w '%{http_code}\n' "$BASE_URL/api/v1/procurements/missing" \
  ${AUTH[@]+"${AUTH[@]}"}

echo "OK"
