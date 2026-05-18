#!/usr/bin/env bash

CONTAINER_NAME=$1
TIMEOUT=${2:-30}

if [ -z "$CONTAINER_NAME" ]; then
  echo "Usage: $0 <container_name> [timeout]"
  exit 1
fi

echo "Waiting for container $CONTAINER_NAME to become healthy (timeout: ${TIMEOUT}s)..."

for i in $(seq 1 "$TIMEOUT"); do
  STATUS=$(docker inspect --format='{{json .State.Health.Status}}' "$CONTAINER_NAME" 2>/dev/null | tr -d '"')
  
  # If the container has no healthcheck, .State.Health will be nil, so STATUS might be null or empty
  if [ -z "$STATUS" ] || [ "$STATUS" == "null" ]; then
     # Check if it's running at least
     RUNNING=$(docker inspect --format='{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null)
     if [ "$RUNNING" == "true" ]; then
       echo "Container $CONTAINER_NAME is running (no healthcheck configured)."
       exit 0
     else
       echo "Container $CONTAINER_NAME is not running."
       exit 1
     fi
  fi

  if [ "$STATUS" == "healthy" ]; then
    echo "Container $CONTAINER_NAME is healthy!"
    exit 0
  elif [ "$STATUS" == "unhealthy" ]; then
    echo "Container $CONTAINER_NAME is unhealthy."
    exit 1
  fi
  sleep 1
done

echo "Timeout waiting for container $CONTAINER_NAME."
exit 1
