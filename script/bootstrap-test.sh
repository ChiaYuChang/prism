#!/usr/bin/env bash
set -euo pipefail

# Bootstrap an already-running local test stack without invoking LLM or search
# providers. Pipeline execution is intentionally a separate operator step.

API_URL="${PRISM_API_URL:-http://localhost:8091/api/v1}"
ADMIN_TOKEN_FILE="${PRISM_ADMIN_TOKEN_FILE:-.secrets/prism_admin_token}"
MODEL_NAME="${PRISM_TEST_MODEL_NAME:-gemma4:31b-cloud}"
MODEL_PROVIDER="${PRISM_TEST_MODEL_PROVIDER:-ollama}"
MODEL_TYPE="${PRISM_TEST_MODEL_TYPE:-EXTRACTOR}"
PROMPT_NAME="${PRISM_TEST_PROMPT_NAME:-worker/planner/analysis/extractor}"
PROMPT_FILE="${PRISM_TEST_PROMPT_FILE:-assets/worker/planner/extractor_v2.md}"
PROMPT_API_NAME="${PROMPT_NAME//\//.}"

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        printf 'missing command: %s\n' "$1" >&2
        exit 1
    fi
}

require_command curl
require_command jq
require_command sha256sum
require_command go
require_command task

if [[ ! -f "$PROMPT_FILE" ]]; then
    printf 'missing prompt file: %s\n' "$PROMPT_FILE" >&2
    exit 1
fi

health_url="${API_URL%/api/v1}/readyz"
printf 'waiting for API readiness: %s\n' "$health_url"
for _ in $(seq 1 "${PRISM_API_WAIT_ATTEMPTS:-60}"); do
    if curl -fsS "$health_url" >/dev/null 2>&1; then
        break
    fi
    sleep "${PRISM_API_WAIT_SECONDS:-2}"
done
if ! curl -fsS "$health_url" >/dev/null 2>&1; then
    printf 'API did not become ready: %s\n' "$health_url" >&2
    exit 1
fi

printf 'initializing root and admin credentials\n'
export PRISM_ADMIN_TOKEN_FILE="$ADMIN_TOKEN_FILE"
task app:auth:bootstrap

if [[ ! -f "$ADMIN_TOKEN_FILE" ]]; then
    printf 'admin token file was not created: %s\n' "$ADMIN_TOKEN_FILE" >&2
    exit 1
fi

printf 'synchronizing sources through the admin API\n'
PRISM_API_URL="$API_URL" task app:sources:bootstrap

admin_token="$(tr -d '\r\n' <"$ADMIN_TOKEN_FILE")"
if [[ -z "$admin_token" ]]; then
    printf 'admin token file is empty: %s\n' "$ADMIN_TOKEN_FILE" >&2
    exit 1
fi

prismctl=(task prismctl:cli -- --output json)

ensure_model() {
    local models
    models="$("${prismctl[@]}" admin models list --limit 500)"
    if jq -e --arg name "$MODEL_NAME" --arg provider "$MODEL_PROVIDER" --arg type "$MODEL_TYPE" \
        '.ok and (.result.items[]? | select(.name == $name and .provider == $provider and .type == $type))' \
        <<<"$models" >/dev/null; then
        printf 'model already registered: %s (%s/%s)\n' "$MODEL_NAME" "$MODEL_PROVIDER" "$MODEL_TYPE"
        return
    fi

    printf 'registering model: %s (%s/%s)\n' "$MODEL_NAME" "$MODEL_PROVIDER" "$MODEL_TYPE"
    "${prismctl[@]}" \
        admin models create \
        --name "$MODEL_NAME" \
        --provider "$MODEL_PROVIDER" \
        --type "$MODEL_TYPE"
}

ensure_prompt() {
    local hash prompts encoded_name
    hash="sha256:$(sha256sum "$PROMPT_FILE" | cut -d' ' -f1)"
    prompts="$("${prismctl[@]}" admin prompts list --name "$PROMPT_API_NAME" --limit 50)"
    if jq -e --arg hash "$hash" '.ok and (.result.items[]? | select(.hash == $hash))' <<<"$prompts" >/dev/null; then
        printf 'prompt version already registered: %s (%s)\n' "$PROMPT_NAME" "$hash"
        return
    fi

    printf 'uploading prompt version: %s (%s)\n' "$PROMPT_NAME" "$hash"
    "${prismctl[@]}" \
        admin prompts upload \
        --name "$PROMPT_NAME" \
        --file "$PROMPT_FILE" \
        --hash "$hash"
}

ensure_model
ensure_prompt

cat <<'EOF'

Bootstrap completed without calling an LLM or search provider.
Next steps:
  1. Confirm scheduler, discovery, and collector workers are running.
  2. Seed or submit a deterministic DIRECTORY_FETCH/PAGE_FETCH test task.
  3. Inspect candidates, contents, archives, logs, and metrics.
  4. Start the planner only when an LLM-backed test is intended.
EOF
