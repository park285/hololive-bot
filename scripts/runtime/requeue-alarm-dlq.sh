#!/usr/bin/env bash
set -euo pipefail

container_name="${VALKEY_CONTAINER_NAME:-valkey-cache}"
socket_path="${VALKEY_SOCKET_PATH:-/var/run/valkey/valkey-cache.sock}"
dlq_key="${ALARM_DLQ_KEY:-alarm:dispatch:dlq}"
queue_key="${ALARM_QUEUE_KEY:-alarm:dispatch:queue}"
max_items="${MAX_ITEMS:-100}"
dry_run="${DRY_RUN:-true}"

if [[ ! "$max_items" =~ ^[0-9]+$ ]] || (( max_items <= 0 )); then
  echo "error: MAX_ITEMS must be a positive integer" >&2
  exit 1
fi

if [[ "$dry_run" != "true" && "${CONFIRM_REQUEUE:-}" != "yes" ]]; then
  echo "refusing to requeue without CONFIRM_REQUEUE=yes" >&2
  echo "run DRY_RUN=true MAX_ITEMS=1 first to inspect the next DLQ item" >&2
  exit 1
fi

replayed=0

while (( replayed < max_items )); do
  if [[ "$dry_run" == "true" ]]; then
    item="$(docker exec "$container_name" valkey-cli -s "$socket_path" LINDEX "$dlq_key" -1 | tr -d '\r')"
    if [[ -z "$item" || "$item" == "(nil)" ]]; then
      break
    fi

    echo "[dry-run] would requeue one item from $dlq_key to $queue_key:"
    echo "$item"
    break
  fi

  moved="$(docker exec "$container_name" valkey-cli -s "$socket_path" RPOPLPUSH "$dlq_key" "$queue_key" | tr -d '\r')"
  if [[ -z "$moved" || "$moved" == "(nil)" ]]; then
    break
  fi

  replayed=$((replayed + 1))
done

echo "requeued=$replayed max_items=$max_items dry_run=$dry_run dlq_key=$dlq_key queue_key=$queue_key"
