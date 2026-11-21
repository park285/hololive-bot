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
slot_collector_unit="${slot_collector_unit:?collector slot preamble required}" slot_host_env="${slot_host_env:?collector slot preamble required}" slot_root="${slot_root:?collector slot preamble required}"

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
    printf '%s\n' "$AP_SLOT_PREAMBLE"
    cat "$REPO_ROOT/scripts/deploy/lib/compose-env.sh"
    cat "$REPO_ROOT/scripts/deploy/lib/postgres-endpoint.sh"
    cat "$RELEASE_PATH_LIB"
    cat "$ROLLBACK_CHECK_LIB"
    cat "$PO_ROLLBACK_LIB"
    cat <<'REMOTE'
set -euo pipefail
service="$1"
unit="$slot_collector_unit"
current="$slot_root/current"
previous="$slot_root/previous"
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
native_rollback_endpoint_validate "$previous_target" "$slot_host_env"
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
  printf '%s\n' "$AP_SLOT_PREAMBLE"
  cat "$REPO_ROOT/scripts/deploy/lib/compose-env.sh"
  cat "$REPO_ROOT/scripts/deploy/lib/postgres-endpoint.sh"
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
unit="$slot_collector_unit"
current="$slot_root/current"
previous="$slot_root/previous"
host_env="$slot_host_env"
unit_file="/etc/systemd/system/${slot_collector_unit%%@*}@.service"
previous_target="$(readlink -f "$previous" 2>/dev/null || true)"
rollback_contract_dir="$previous_target/rollback-contract"

if [[ -z "$previous_target" || ! -d "$previous_target" ]]; then
  echo "no previous collector release to roll back to; fix forward" >&2
  exit 1
fi
native_rollback_validate "$previous_target"
native_rollback_endpoint_validate "$previous_target" "$host_env"
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
sudo -n install -m 0640 -o root -g root "$rollback_contract_dir/${host_env##*/}" "$host_env"
sudo -n install -m 0644 -o root -g root "$rollback_contract_dir/${unit_file##*/}" "$unit_file"
sudo -n ln -sfn "$previous_target" "$current"
native_previous_link_restore "$slot_root/releases" "$previous" "$rollback_contract_dir/previous-before-cutover"
po_restore_previous "$previous_target"
sudo -n systemd-analyze verify "$unit_file"
sudo -n systemctl daemon-reload
sudo -n systemctl enable --now "$unit"
echo "rollback_started_at=$rollback_started_at"
native_cutover_release_guard
REMOTE
} | ap_remote_bash "$service" "$rollback_started_at"

po_expected_presence="$(ap_remote_bash "$slot_root/current/rollback-contract/po-unit-presence" <<'REMOTE'
set -euo pipefail
sudo -n cat "$1"
REMOTE
)"
PO_EXPECTED_PRESENCE="$po_expected_presence" CHANGE_STARTED_AT="$rollback_started_at" \
  "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"
