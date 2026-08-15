#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
MODE="${2:---dry-run}"
ROLLBACK_CHECK_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-rollback-check.sh"
RETIRED_PRODUCER_LIB="$REPO_ROOT/scripts/deploy/lib/retired-producer-cutover.sh"

case "$MODE" in
  --dry-run|--apply) ;;
  *)
    echo "Usage: $0 <ap-host> [--dry-run|--apply]" >&2
    exit 2
    ;;
esac

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ "$AP_RUNTIME_MODE" != "native" ]]; then
  echo "Refusing host-native AP rollback for $AP_NAME (runtime=$AP_RUNTIME_MODE); use ./scripts/deploy/ap-rollback.sh $AP_NAME" >&2
  exit 2
fi

if [[ ${#AP_SERVICES[@]} -ne 1 || ${#AP_PORTS[@]} -ne 1 ]]; then
  echo "host-native rollback currently supports exactly one AP service per host" >&2
  exit 2
fi

if [[ "$MODE" == "--apply" && "${!AP_APPROVE_ROLLBACK_VAR:-}" != "true" ]]; then
  echo "Refusing apply without $AP_APPROVE_ROLLBACK_VAR=true" >&2
  exit 2
fi

service="${AP_SERVICES[0]}"

if [[ "$MODE" == "--dry-run" ]]; then
  {
    cat "$ROLLBACK_CHECK_LIB"
    cat "$RETIRED_PRODUCER_LIB"
    cat <<'REMOTE'
set -euo pipefail
service="$1"
unit="hololive-youtube-collector@${service}.service"
current="/opt/hololive-bot/youtube-collector/current"
previous="/opt/hololive-bot/youtube-collector/previous"
previous_target="$(readlink -f "$previous" 2>/dev/null || true)"
rollback_contract_dir="$previous_target/rollback-contract"
producer_state_file="/opt/hololive-bot/youtube-collector/releases/first-cutover-producer.state"
echo "unit=$unit"
echo "current=$(readlink -f "$current" 2>/dev/null || true)"
echo "previous=$previous_target"
if [[ -n "$previous_target" && -d "$previous_target" ]]; then
  native_rollback_validate "$previous_target"
  echo "[DRY-RUN] Previous payload, host env, and systemd unit passed rollback validation."
else
  validate_retired_producer_runtime_state "$producer_state_file" "$service"
  echo "[DRY-RUN] Recorded first-cutover producer state passed rollback validation."
fi
REMOTE
  } | ap_remote_bash "$service"
  exit 0
fi

rollback_mode="$(ap_remote_bash <<'REMOTE'
previous="/opt/hololive-bot/youtube-collector/previous"
previous_target="$(readlink -f "$previous" 2>/dev/null || true)"
if [[ -n "$previous_target" && -d "$previous_target" ]]; then
  printf '%s\n' collector
else
  printf '%s\n' producer
fi
REMOTE
)"

rollback_started_at="$(ap_remote_bash <<'REMOTE'
date -u +%Y-%m-%dT%H:%M:%SZ
REMOTE
)"

{
  cat "$ROLLBACK_CHECK_LIB"
  cat "$RETIRED_PRODUCER_LIB"
  cat <<'REMOTE'
set -euo pipefail
service="$1"
rollback_started_at="$2"
unit="hololive-youtube-collector@${service}.service"
current="/opt/hololive-bot/youtube-collector/current"
previous="/opt/hololive-bot/youtube-collector/previous"
host_env="/etc/hololive-bot/youtube-collector-host.env"
unit_file="/etc/systemd/system/hololive-youtube-collector@.service"
previous_target="$(readlink -f "$previous" 2>/dev/null || true)"
rollback_contract_dir="$previous_target/rollback-contract"
producer_state_file="/opt/hololive-bot/youtube-collector/releases/first-cutover-producer.state"

if [[ -n "$previous_target" && -d "$previous_target" ]]; then
  native_rollback_validate "$previous_target"
  sudo -n install -m 0640 -o root -g root "$rollback_contract_dir/youtube-collector-host.env" "$host_env"
  sudo -n install -m 0644 -o root -g root "$rollback_contract_dir/hololive-youtube-collector@.service" "$unit_file"
  sudo -n ln -sfn "$previous_target" "$current"
  sudo -n systemd-analyze verify "$unit_file"
  sudo -n systemctl daemon-reload
  sudo -n systemctl restart "$unit"
else
  validate_retired_producer_runtime_state "$producer_state_file" "$service"
  stop_named_units_and_require_inactive "$unit"
  restore_retired_producer_runtime "$producer_state_file" "$service"
fi
echo "rollback_started_at=$rollback_started_at"
REMOTE
} | ap_remote_bash "$service" "$rollback_started_at"

if [[ "$rollback_mode" == "collector" ]]; then
  CHANGE_STARTED_AT="$rollback_started_at" \
    "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"
else
  echo "first-cutover producer runtime rollback verified"
fi
