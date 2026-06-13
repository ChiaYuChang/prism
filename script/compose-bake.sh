#!/usr/bin/env bash
set -euo pipefail
umask 077

# --- Configuration Defaults ---
ENV=${ENV:-test}
PROFILES=${PROFILES:-"dev,obs"}
DEPLOY_DIR=${DEPLOY_DIR:-"deployments"}
REPO_ROOT=$(pwd)

# --- Validation ---

# 1. Validate ENV
if [[ "$ENV" != "test" && "$ENV" != "prod" ]]; then
  echo "Error: ENV must be 'test' or 'prod'. Found: '$ENV'" >&2
  exit 1
fi

# 2. Prod gate
if [[ "$ENV" == "prod" ]] && [[ "${PRISM_PROD_OK:-0}" != "1" ]]; then
  echo "refuse: ENV=prod requires PRISM_PROD_OK=1" >&2
  exit 1
fi

# --- Output Path ---
PROFILES_SLUG=$(echo "$PROFILES" | tr ',' '_')
OUTPUT_FILE="${REPO_ROOT}/.${ENV}-${PROFILES_SLUG}.docker-compose.merged.yaml"

# --- Temp File + Cleanup Trap ---
TMP_FILE=""
cleanup() {
  if [ -n "${TMP_FILE:-}" ] && [ -f "$TMP_FILE" ]; then
    rm -f "$TMP_FILE"
  fi
}
trap cleanup EXIT

TMP_FILE=$(mktemp -p "$REPO_ROOT" docker-compose-XXXXXX.yaml)

# --- Compose Source Files ---
COMPOSE_FLAGS=(
  -f "${DEPLOY_DIR}/docker-compose.yaml"
  -f "${DEPLOY_DIR}/docker-compose.${ENV}.yaml"
  -f "${DEPLOY_DIR}/docker-compose.tool.yaml"
  -f "${DEPLOY_DIR}/docker-compose.worker.yaml"
  -f "${DEPLOY_DIR}/docker-compose.app.yaml"
)

# --- Merge ---
# COMPOSE_PROFILES filters which profile-gated services land in the merged
# output. --no-interpolate keeps ${VAR} references literal in the merged
# file so secret values are resolved at `compose up` time (when the
# Taskfile env block provides them) rather than baked into the file as
# plaintext. 0600 mode is kept as defense-in-depth.
export COMPOSE_PROFILES="$PROFILES"

echo "Baking ENV=$ENV PROFILES=$PROFILES -> $OUTPUT_FILE" >&2
docker compose "${COMPOSE_FLAGS[@]}" config --no-interpolate > "$TMP_FILE"
chmod 0600 "$TMP_FILE"
mv -f "$TMP_FILE" "$OUTPUT_FILE"
TMP_FILE=""  # successfully renamed; prevent trap from deleting final file

echo "$OUTPUT_FILE"
