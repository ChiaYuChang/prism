#!/usr/bin/env sh
set -eu

API_URL="${PRISM_API_URL:-http://localhost:8091/api/v1}"
HEALTH_URL="${PRISM_HEALTH_URL:-${API_URL%/api/v1}/readyz}"
MANIFEST="${PRISM_SOURCE_MANIFEST:-configs/registry/sources.yaml}"
ADMIN_TOKEN_FILE="${PRISM_ADMIN_TOKEN_FILE:-.secrets/prism_admin_token}"

i=0
while ! curl -fsS "$HEALTH_URL" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge "${PRISM_API_WAIT_ATTEMPTS:-60}" ]; then
    printf '%s\n' "API did not become healthy: $HEALTH_URL" >&2
    exit 1
  fi
  sleep "${PRISM_API_WAIT_SECONDS:-2}"
done

if [ -f "$ADMIN_TOKEN_FILE" ]; then
  exec go run ./cmd/prismctl --api-url "$API_URL" --admin-token-file "$ADMIN_TOKEN_FILE" admin sources sync --manifest "$MANIFEST"
fi
exec go run ./cmd/prismctl --api-url "$API_URL" admin sources sync --manifest "$MANIFEST"
