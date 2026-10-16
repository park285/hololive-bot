#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BACKUP_DIR="${BACKUP_DIR:-}"
. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"
MODE="${2:---dry-run}"
[[ "$MODE" == --dry-run || "$MODE" == --apply ]] || { echo "Usage: $0 <ap-host> [--dry-run|--apply]" >&2; exit 2; }
[[ "$AP_RUNTIME_MODE" == compose && "$AP_NAME" == seoul && ${#AP_SERVICES[@]} -eq 1 ]] || {
  echo 'PO rollback supports only the Seoul Compose collector; use native rollback on a/d' >&2
  exit 2
}
if [[ "$MODE" == --apply && "${!AP_APPROVE_ROLLBACK_VAR:-}" != true ]]; then
  echo "Refusing rollback without $AP_APPROVE_ROLLBACK_VAR=true" >&2
  exit 2
fi
if [[ -z "$BACKUP_DIR" ]]; then
  BACKUP_DIR="$("${AP_SSH[@]}" "find ~/hololive-bot/backups -maxdepth 1 -type d -name '$AP_BACKUP_PREFIX-*' | sort | tail -n 1" | sed 's#^.*/backups/#backups/#')"
fi
[[ "$BACKUP_DIR" =~ ^backups/${AP_BACKUP_PREFIX}-[0-9]{8}T[0-9]{6}Z$ ]] || {
  echo "Invalid or unavailable AP rollback directory: $BACKUP_DIR" >&2
  exit 2
}
remote_script="\$HOME/hololive-bot/$BACKUP_DIR/source-candidate/hololive-bot/scripts/deploy/lib/po-ap-rollback-remote.sh"
remote_args="$(printf '%q ' "$BACKUP_DIR" "$AP_COMPOSE_FILE" "${AP_SERVICES[0]}" "${AP_CONTAINERS[0]}" "${AP_PORTS[0]}")"
"${AP_SSH[@]}" "bash \"$remote_script\" check $remote_args ''"
if [[ "$MODE" == --dry-run ]]; then
  echo "[DRY-RUN] Both previous image/rootfs manifests and Compose config verified: $BACKUP_DIR"
  exit 0
fi
rollback_started_at="$("${AP_SSH[@]}" 'date -u +%Y-%m-%dT%H:%M:%SZ')"
apply_output="$("${AP_SSH[@]}" "bash \"$remote_script\" apply $remote_args $(printf '%q' "$rollback_started_at")")"
printf '%s\n' "$apply_output"
# 점검 창에서 정지했던 collector는 원격 apply가 created 상태와 revision을 확인하고 시작하지 않는다.
# health를 조회할 실행 중 container가 없으므로 상태 조회 대신 재개 조건을 안내한다.
if [[ "$apply_output" == *' collector_state=stopped '* ]]; then
  echo "[ROLLBACK] $AP_NAME collector restored stopped; start it only after the schema matches the restored image"
  exit 0
fi
# 원격 check가 rollback-image-tag 없는 백업을 거절하므로 여기서는 이전 collector가 실행 중이다.
"$REPO_ROOT/scripts/logs/ap-status.sh" "$AP_NAME"
