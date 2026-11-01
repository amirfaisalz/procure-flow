#!/usr/bin/env bash
# Generate an HS256 signing secret for local session logins and store it in the
# env file loaded by `make run`.
#
# The secret is 32 random bytes rendered as 64 hex characters, which satisfies
# the 32-character minimum enforced by the configuration loader. The existing
# `AUTH_JWT_SECRET` line is replaced in place (whether active or commented out);
# when the file has no such line the value is appended.
#
# Usage:
#   scripts/gen-jwt-secret.sh
#   ENV_FILE=/path/to/file scripts/gen-jwt-secret.sh
set -eu

file="${ENV_FILE:-.env}"

if [[ ! -f "$file" ]]; then
	echo "error: $file not found; copy .env.example to .env first" >&2
	exit 1
fi

if command -v openssl >/dev/null 2>&1; then
	secret="$(openssl rand -hex 32)"
else
	secret="$(LC_ALL=C tr -dc 'a-f0-9' </dev/urandom | head -c 64)"
fi

if [[ ${#secret} -ne 64 ]]; then
	echo "error: could not generate a 32-byte secret" >&2
	exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

awk -v secret="$secret" '
	/^[[:space:]]*#?[[:space:]]*AUTH_JWT_SECRET=/ && !done {
		print "AUTH_JWT_SECRET=" secret
		done = 1
		next
	}
	{ print }
	END { if (!done) print "AUTH_JWT_SECRET=" secret }
' "$file" >"$tmp"

# Overwrite in place so the file keeps its existing mode and ownership.
cat "$tmp" >"$file"

echo "AUTH_JWT_SECRET written to $file (64 hex characters)"
echo "restart 'make run' to use it; existing login sessions become invalid"
