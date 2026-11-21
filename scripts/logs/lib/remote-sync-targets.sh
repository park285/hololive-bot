#!/usr/bin/env bash
# shellcheck disable=SC2034

# shellcheck source=scripts/deploy/lib/collector-slots.sh
. "${REPO_ROOT:?}/scripts/deploy/lib/collector-slots.sh"

remote_sync_collector_services() {
  local table="${REPO_ROOT}/scripts/deploy/lib/collector-slots.tsv" slot
  for slot in $(collector_slots_where host "$1" "$table"); do
    collector_slot "$slot" service "$table"
  done | paste -sd ' ' -
}

CENTRAL_USER_HOST="${HOL_LOG_CENTRAL_USER_HOST:-ubuntu@100.100.1.8}"
CENTRAL_SSH_KEY="${HOL_LOG_CENTRAL_SSH_KEY:-${HOME}/.ssh/KR.key}"
CENTRAL_HOST_KEY_ALIAS="${HOL_LOG_CENTRAL_HOST_KEY_ALIAS:-100.100.1.8}"
CENTRAL_REMOTE_LOG_DIR="${HOL_LOG_CENTRAL_LOG_DIR:-/var/log/hololive-bot}"
CENTRAL_SERVICES="${HOL_LOG_CENTRAL_SERVICES:-hololive-api alarm-worker $(remote_sync_collector_services central)}"
OSAKA_USER_HOST="${HOL_LOG_OSAKA_USER_HOST:-ubuntu@100.100.1.6}"
OSAKA_SSH_KEY="${HOL_LOG_OSAKA_SSH_KEY:-${HOME}/.ssh/KR.key}"
OSAKA_HOST_KEY_ALIAS="${HOL_LOG_OSAKA_HOST_KEY_ALIAS:-100.100.1.6}"
OSAKA_REMOTE_LOG_DIR="${HOL_LOG_OSAKA_LOG_DIR:-/var/log/hololive-bot}"
OSAKA_SERVICES="${HOL_LOG_OSAKA_SERVICES:-$(remote_sync_collector_services osaka)}"
SEOUL_USER_HOST="${HOL_LOG_SEOUL_USER_HOST:-ubuntu@100.100.1.5}"
SEOUL_SSH_KEY="${HOL_LOG_SEOUL_SSH_KEY:-${HOME}/.ssh/KR.key}"
SEOUL_HOST_KEY_ALIAS="${HOL_LOG_SEOUL_HOST_KEY_ALIAS:-100.100.1.5}"
SEOUL_REMOTE_LOG_DIR="${HOL_LOG_SEOUL_LOG_DIR:-/var/lib/hololive-collectors/b/logs}"
SEOUL_SERVICES="${HOL_LOG_SEOUL_SERVICES:-$(remote_sync_collector_services seoul)}"
