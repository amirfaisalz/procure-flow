#!/usr/bin/env bash
# Print the local endpoints and start the API.
#
# Invoked by `make run` through scripts/load-env.sh, so the values from .env are
# already in the environment and HTTP_ADDR determines the advertised URL.
set -eu

cd "$(dirname "$0")/.."

addr="${HTTP_ADDR:-:8080}"
host="${addr%:*}"
port="${addr##*:}"

case "$host" in
	"" | "0.0.0.0" | "[::]" | "::") host=localhost ;;
esac

base="http://${host}:${port}"

printf 'procure-flow API   %s\n' "$base"
printf 'API reference      %s/docs\n' "$base"
printf 'health             %s/healthz\n' "$base"
printf 'environment        %s\n\n' "${APP_ENV:-development}"

exec go run ./cmd/api
