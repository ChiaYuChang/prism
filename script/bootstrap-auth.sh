#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ "${PRISM_BREAK_GLASS:-0}" != "1" ]]; then
    printf '%s\n' 'refuse root bootstrap: set PRISM_BREAK_GLASS=1' >&2
    exit 1
fi

ROOT_FILE="${PRISM_ROOT_TOKEN_FILE:-.secrets/prism_root_token}"
ADMIN_FILE="${PRISM_ADMIN_TOKEN_FILE:-.secrets/prism_admin_token}"
PG_PASSWORD_FILE="${PG_PASSWORD_FILE:-.secrets/pg-prism}"
export PRISM_ROOT_TOKEN_FILE="$ROOT_FILE"
export PRISM_ADMIN_TOKEN_FILE="$ADMIN_FILE"

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

root_cli=(env PRISM_BREAK_GLASS=1 task --silent prismctl:root --)
api_cli=(task --silent prismctl:cli --)

if [[ ! -f "$ROOT_FILE" ]]; then
    mkdir -p "$(dirname "$ROOT_FILE")"
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n' >"$ROOT_FILE"
    chmod 0444 "$ROOT_FILE"
fi

if ! "${root_cli[@]}" check >/dev/null 2>&1; then
    "${root_cli[@]}" init >/dev/null
fi

admin_valid=false
if [[ -f "$ADMIN_FILE" ]]; then
    # Docker Compose file-backed secrets preserve the source file mode. The
    # owner-only .secrets directory still protects this host file while the
    # non-root CLI container needs read access to the mounted secret.
    chmod 0444 "$ADMIN_FILE"
    if "${api_cli[@]}" --output json admin tokens list >/dev/null 2>&1; then
        admin_valid=true
    fi
fi

if [[ "$admin_valid" != true ]]; then
    mkdir -p "$(dirname "$ADMIN_FILE")"
    response="$("${root_cli[@]}" --output json admin-create --name local-admin)"
    token="$(printf '%s\n' "$response" | jq -Rer 'select(startswith("{")) | fromjson.result.token')"
    tmp="$(mktemp "${ADMIN_FILE}.XXXXXX")"
    printf '%s\n' "$token" >"$tmp"
    chmod 0444 "$tmp"
    mv "$tmp" "$ADMIN_FILE"
    unset response token
fi

printf '%s\n' 'root and admin tokens are initialized'
