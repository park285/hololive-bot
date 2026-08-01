#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
MODE="${2:---dry-run}"

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
  ap_remote_bash "$service" <<'REMOTE'
set -euo pipefail
service="$1"
unit="hololive-youtube-producer@${service}.service"
current="/opt/hololive-bot/youtube-producer/current"
previous="/opt/hololive-bot/youtube-producer/previous"
previous_target="$(readlink -f "$previous" 2>/dev/null || true)"
rollback_contract_dir="$previous_target/rollback-contract"
echo "unit=$unit"
echo "current=$(readlink -f "$current" 2>/dev/null || true)"
echo "previous=$previous_target"
if [[ -z "$previous_target" || ! -d "$previous_target" ]]; then
  echo "previous host-native release is unavailable; refusing partial rollback" >&2
  exit 1
fi
if ! sudo -n test -r "$rollback_contract_dir/youtube-producer-host.env" ||
   ! sudo -n test -r "$rollback_contract_dir/hololive-youtube-producer@.service"; then
  echo "previous host-native rollback contract is incomplete; refusing partial rollback" >&2
  exit 1
fi
echo "[DRY-RUN] Previous binary, host env, and systemd unit are available for rollback."
REMOTE
  exit 0
fi

rollback_started_at="$(ap_remote_bash <<'REMOTE'
date -u +%Y-%m-%dT%H:%M:%SZ
REMOTE
)"

ap_remote_bash "$service" "$rollback_started_at" <<'REMOTE'
set -euo pipefail
service="$1"
rollback_started_at="$2"
unit="hololive-youtube-producer@${service}.service"
current="/opt/hololive-bot/youtube-producer/current"
previous="/opt/hololive-bot/youtube-producer/previous"
host_env="/etc/hololive-bot/youtube-producer-host.env"
unit_file="/etc/systemd/system/hololive-youtube-producer@.service"
previous_target="$(readlink -f "$previous" 2>/dev/null || true)"
rollback_contract_dir="$previous_target/rollback-contract"

if [[ -z "$previous_target" || ! -d "$previous_target" ]]; then
  echo "previous host-native release is unavailable; refusing partial rollback" >&2
  exit 1
fi
if ! sudo -n test -r "$rollback_contract_dir/youtube-producer-host.env" ||
   ! sudo -n test -r "$rollback_contract_dir/hololive-youtube-producer@.service"; then
  echo "previous host-native rollback contract is incomplete; refusing partial rollback" >&2
  exit 1
fi

sudo -n install -m 0640 -o root -g root "$rollback_contract_dir/youtube-producer-host.env" "$host_env"
sudo -n install -m 0644 -o root -g root "$rollback_contract_dir/hololive-youtube-producer@.service" "$unit_file"
sudo -n ln -sfn "$previous_target" "$current"
sudo -n systemd-analyze verify "$unit_file"
sudo -n systemctl daemon-reload
sudo -n systemctl restart "$unit"
echo "rollback_started_at=$rollback_started_at"
REMOTE

CHANGE_STARTED_AT="$rollback_started_at" \
  "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"
