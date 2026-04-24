#!/usr/bin/env bash
set -euo pipefail

container_name="${VALKEY_CONTAINER_NAME:-valkey-cache}"
socket_path="${VALKEY_SOCKET_PATH:-/var/run/valkey/valkey-cache.sock}"
dlq_key="${ALARM_DLQ_KEY:-alarm:dispatch:dlq}"
queue_key="${ALARM_QUEUE_KEY:-alarm:dispatch:queue}"

replayed=0

while true; do
  moved="$(docker exec "$container_name" valkey-cli -s "$socket_path" RPOPLPUSH "$dlq_key" "$queue_key")"
  if [[ -z "$moved" || "$moved" == "(nil)" ]]; then
    break
  fi
  replayed=$((replayed + 1))
done

echo "requeued $replayed item(s) from $dlq_key to $queue_key"
