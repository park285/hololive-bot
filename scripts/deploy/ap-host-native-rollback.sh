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

if [[ ${#AP_SERVICES[@]} -ne 1 ]]; then
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
echo "unit=$unit"
echo "current=$(readlink -f "$current" 2>/dev/null || true)"
echo "previous=$(readlink -f "$previous" 2>/dev/null || true)"
echo "[DRY-RUN] Would restore previous release if present; otherwise stop the unit."
REMOTE
  exit 0
fi

ap_remote_bash "$service" <<'REMOTE'
set -euo pipefail
service="$1"
unit="hololive-youtube-producer@${service}.service"
current="/opt/hololive-bot/youtube-producer/current"
previous="/opt/hololive-bot/youtube-producer/previous"

if [[ -L "$previous" && -d "$(readlink -f "$previous")" ]]; then
  sudo -n ln -sfn "$(readlink -f "$previous")" "$current"
  sudo -n systemctl restart "$unit"
  systemctl show "$unit" -p ActiveState -p SubState -p ExecMainPID -p MemoryCurrent -p NRestarts -p ActiveEnterTimestamp
else
  sudo -n systemctl disable --now "$unit" || true
  systemctl show "$unit" -p ActiveState -p SubState -p ExecMainPID -p NRestarts || true
fi
REMOTE
