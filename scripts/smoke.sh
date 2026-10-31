#!/usr/bin/env bash
# Smoke-test a running procure-flow instance.
#
# Usage:
#   scripts/smoke.sh [base-url]
#
# Requires: curl, jq.

set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"

echo "==> health"
curl -fsS "$BASE_URL/healthz" | jq .
curl -fsS "$BASE_URL/readyz" | jq .

echo "==> create"
CREATED="$(curl -fsS -X POST "$BASE_URL/api/v1/procurements" \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Laptop procurement for engineering",
    "description": "20 units of development laptops",
    "department": "Engineering",
    "requester": "Amir",
    "amount": 250000000,
    "currency": "IDR"
  }')"
echo "$CREATED" | jq .

ID="$(echo "$CREATED" | jq -r '.data.id')"

echo "==> submit"
curl -fsS -X POST "$BASE_URL/api/v1/procurements/$ID/submit" | jq '.data.status'

echo "==> approve"
curl -fsS -X POST "$BASE_URL/api/v1/procurements/$ID/approve" | jq '.data.status'

echo "==> list"
curl -fsS "$BASE_URL/api/v1/procurements?page=1&page_size=10" | jq '.meta'

echo "==> 404 problem document"
curl -sS -o /dev/null -w '%{http_code}\n' "$BASE_URL/api/v1/procurements/missing"

echo "OK"
