#!/usr/bin/env bash
set -euo pipefail
umask 077

ROOT_FILE="${PRISM_ROOT_TOKEN_FILE:-.secrets/prism_root_token}"
ADMIN_FILE="${PRISM_ADMIN_TOKEN_FILE:-.secrets/prism_admin_token}"
PG_HOST="${PRISMCTL_PG_HOST:-localhost}"
PG_PORT="${PRISMCTL_PG_PORT:-5432}"
PG_USER="${PRISMCTL_PG_USERNAME:-prism}"
PG_DB="${PRISMCTL_PG_DB:-prism}"
PG_PASSWORD_FILE="${PRISMCTL_PG_PASSWORD_FILE:-.secrets/pg-prism}"

if [[ ! -d .secrets ]]; then
    printf '%s\n' '.secrets is missing; run from the repository root' >&2
    exit 1
fi
if [[ ! -f "$PG_PASSWORD_FILE" ]]; then
    printf 'missing PostgreSQL password file: %s\n' "$PG_PASSWORD_FILE" >&2
    exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
    printf '%s\n' 'jq is required to capture the generated admin token safely' >&2
    exit 1
fi

cli=(
    go run ./cmd/prismctl
    --pg-host "$PG_HOST"
    --pg-port "$PG_PORT"
    --pg-username "$PG_USER"
    --pg-password-file "$PG_PASSWORD_FILE"
    --pg-db "$PG_DB"
)

if [[ ! -f "$ROOT_FILE" ]]; then
    mkdir -p "$(dirname "$ROOT_FILE")"
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n' >"$ROOT_FILE"
    chmod 0600 "$ROOT_FILE"
fi

if ! "${cli[@]}" --root-token-file "$ROOT_FILE" root check >/dev/null 2>&1; then
    "${cli[@]}" --root-token-file "$ROOT_FILE" root init >/dev/null
fi

admin_valid=false
if [[ -f "$ADMIN_FILE" ]]; then
    if "${cli[@]}" --admin-token-file "$ADMIN_FILE" --output json admin tokens list >/dev/null 2>&1; then
        admin_valid=true
    fi
fi

if [[ "$admin_valid" != true ]]; then
    mkdir -p "$(dirname "$ADMIN_FILE")"
    response="$("${cli[@]}" --root-token-file "$ROOT_FILE" --output json root admin-create --name local-admin)"
    token="$(printf '%s' "$response" | jq -er '.result.token')"
    tmp="$(mktemp "${ADMIN_FILE}.XXXXXX")"
    printf '%s\n' "$token" >"$tmp"
    chmod 0600 "$tmp"
    mv "$tmp" "$ADMIN_FILE"
    unset response token
fi

printf '%s\n' 'root and admin tokens are initialized'
