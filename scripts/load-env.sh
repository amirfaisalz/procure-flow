#!/usr/bin/env bash
# Run a command with configuration loaded from a local env file.
#
# The file is parsed literally: there is no shell expansion, so values such as
# URLs containing `$` or `?` are kept as written. Blank lines and `#` comments
# are skipped, an optional `export ` prefix is accepted, and one layer of
# matching single or double quotes is removed.
#
# Values already present in the environment are never overridden, so
# `DATABASE_URL=... make run` and CI-provided settings win over the file.
#
# Usage:
#   scripts/load-env.sh <command> [args...]
#   ENV_FILE=/path/to/file scripts/load-env.sh <command> [args...]
#
# The API, worker, and migration binaries never read this file themselves;
# production receives the same variables from its process manager.
set -eu

file="${ENV_FILE:-.env}"

if [[ -f "$file" ]]; then
	while IFS= read -r line || [[ -n "$line" ]]; do
		line="${line%$'\r'}"
		[[ "$line" =~ ^[[:space:]]*(#|$) ]] && continue
		line="${line#"${line%%[![:space:]]*}"}"
		line="${line#export }"
		[[ "$line" == *=* ]] || continue

		name="${line%%=*}"
		name="${name//[[:space:]]/}"
		value="${line#*=}"

		[[ "$name" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue

		if [[ ${#value} -ge 2 && ( "$value" == \"*\" || "$value" == \'*\' ) ]]; then
			value="${value:1:${#value}-2}"
		fi

		if [[ -z "${!name+x}" ]]; then
			export "$name=$value"
		fi
	done <"$file"
fi

exec "$@"
