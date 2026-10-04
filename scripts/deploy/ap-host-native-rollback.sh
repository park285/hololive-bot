#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
MODE="${2:---dry-run}"
ROLLBACK_CHECK_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-rollback-check.sh"
RELEASE_PATH_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-release-path.sh"
PO_ROLLBACK_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-po.sh"
NATIVE_CUTOVER_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-cutover.sh"

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

# rollback 기준점은 previous collector release 하나다. previous가 없을 때 퇴역 producer의 첫 cutover 상태를 복원하던 경로는
# T18(2026-09-26)에서 osaka1·osaka2의 current·previous가 모두 collector release이고 producer unit이 0개임을 확인해
# 지웠다(stack-audit T11 holo-collector-retired-producer-cutover-tooling). previous가 없으면 되돌릴 대상이 없으므로 거절한다.
if [[ "$MODE" == "--dry-run" ]]; then
  {
    cat "$RELEASE_PATH_LIB"
    cat "$ROLLBACK_CHECK_LIB"
    cat "$PO_ROLLBACK_LIB"
    cat <<'REMOTE'
set -euo pipefail
service="$1"
unit="hololive-youtube-collector@${service}.service"
current="/opt/hololive-bot/youtube-collector/current"
previous="/opt/hololive-bot/youtube-collector/previous"
previous_target="$(readlink -f "$previous" 2>/dev/null || true)"
rollback_contract_dir="$previous_target/rollback-contract"
echo "unit=$unit"
echo "current=$(readlink -f "$current" 2>/dev/null || true)"
echo "previous=$previous_target"
if [[ -z "$previous_target" || ! -d "$previous_target" ]]; then
  echo "no previous collector release to roll back to; fix forward" >&2
  exit 1
fi
native_rollback_validate "$previous_target"
echo "[DRY-RUN] Previous payload, host env, and systemd unit passed rollback validation."
REMOTE
  } | ap_remote_bash "$service"
  exit 0
fi

rollback_started_at="$(ap_remote_bash <<'REMOTE'
date -u +%Y-%m-%dT%H:%M:%SZ
REMOTE
)"

{
  cat "$RELEASE_PATH_LIB"
  cat "$ROLLBACK_CHECK_LIB"
  cat "$PO_ROLLBACK_LIB"
  cat "$NATIVE_CUTOVER_LIB"
  cat <<'REMOTE'
set -euo pipefail
service="$1"
rollback_started_at="$2"
native_recovery_require_clear
native_cutover_claim manual-rollback
unit="hololive-youtube-collector@${service}.service"
current="/opt/hololive-bot/youtube-collector/current"
previous="/opt/hololive-bot/youtube-collector/previous"
host_env="/etc/hololive-bot/youtube-collector-host.env"
unit_file="/etc/systemd/system/hololive-youtube-collector@.service"
previous_target="$(readlink -f "$previous" 2>/dev/null || true)"
rollback_contract_dir="$previous_target/rollback-contract"

if [[ -z "$previous_target" || ! -d "$previous_target" ]]; then
  echo "no previous collector release to roll back to; fix forward" >&2
  exit 1
fi
native_rollback_validate "$previous_target"
# 이전 collector와 issuer를 같은 release로 되돌리기 전에 현재 issuer와 collector를 멈추고 비활성을 확인한다.
# cutover와 같이 issuer를 먼저 멈춰 collector의 종료 generation 반납이 broker 재시작을 만들지 않게 한다.
if sudo -n systemctl is-active --quiet "$po_socket"; then
  sudo -n systemctl stop "$po_socket"
fi
if sudo -n systemctl is-active --quiet "$po_service"; then
  sudo -n systemctl stop "$po_service"
fi
if systemctl cat "$unit" >/dev/null 2>&1; then
  sudo -n systemctl disable --now "$unit" >/dev/null
fi
if systemctl is-active --quiet "$unit" 2>/dev/null; then
  echo "collector unit still active: $unit" >&2
  exit 1
fi
sudo -n install -m 0640 -o root -g root "$rollback_contract_dir/youtube-collector-host.env" "$host_env"
sudo -n install -m 0644 -o root -g root "$rollback_contract_dir/hololive-youtube-collector@.service" "$unit_file"
sudo -n ln -sfn "$previous_target" "$current"
native_previous_link_restore /opt/hololive-bot/youtube-collector/releases "$previous" "$rollback_contract_dir/previous-before-cutover"
po_restore_previous "$previous_target"
sudo -n systemd-analyze verify "$unit_file"
sudo -n systemctl daemon-reload
sudo -n systemctl enable --now "$unit"
echo "rollback_started_at=$rollback_started_at"
native_cutover_release_guard
REMOTE
} | ap_remote_bash "$service" "$rollback_started_at"

po_expected_presence="$(ap_remote_bash <<'REMOTE'
set -euo pipefail
sudo -n cat /opt/hololive-bot/youtube-collector/current/rollback-contract/po-unit-presence
REMOTE
)"
PO_EXPECTED_PRESENCE="$po_expected_presence" CHANGE_STARTED_AT="$rollback_started_at" \
  "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"
