#!/usr/bin/env bash
set -euo pipefail

tool_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
slot="${1:-}" action="${2:-}"
if [[ ! "$slot" =~ ^[abcd]$ || ! "$action" =~ ^(stop|start)$ || $# -ne 2 ]]; then
  echo "Usage: $0 <a|b|c|d> <stop|start>" >&2
  exit 2
fi
if [[ "${APPROVE_COLLECTOR_PAUSE:-}" != true ]]; then
  echo "Refusing without APPROVE_COLLECTOR_PAUSE=true" >&2
  exit 2
fi

unit="$(awk -F'\t' -v slot="$slot" 'NR > 1 && $1 == slot { print $9 }' "$tool_root/scripts/deploy/lib/collector-slots.tsv")"
[[ "$unit" =~ ^hololive-collector[@a-z-]*\.service$ ]] || { echo "collector unit for slot $slot not found" >&2; exit 2; }

recovery=""
case "$slot" in
  a)
    . "$tool_root/scripts/deploy/lib/ap-host.sh"
    ap_host_load "$tool_root" osaka
    recovery="${slot_root:?collector slot preamble required}/cutover-recovery"
    remote() { ap_remote_bash "$@"; }
    ;;
  b|c)
    target_var="PO_${slot^^}_SSH_TARGET"
    target="${!target_var:-}"
    [[ -n "$target" ]] || { echo "$target_var required" >&2; exit 2; }
    remote() { ssh -o BatchMode=yes "$target" "bash -s -- $(printf '%q ' "$@")"; }
    ;;
  d)
    remote() { bash -s -- "$@"; }
    ;;
esac

output="$(remote "$unit" "$action" "$recovery" <<'REMOTE'
set -euo pipefail
unit="$1" action="$2" recovery="$3"
prop() { sudo -n systemctl show -p "$1" --value "$unit"; }
report() { local p; for p in ActiveState SubState Result MainPID NRestarts Job; do printf '%s=%s\n' "$p" "$(prop "$p")"; done; }
if [[ -n "$recovery" ]] && sudo -n test -e "$recovery"; then
  echo "native cutover recovery state present: $recovery" >&2
  exit 1
fi
[[ "$(prop LoadState)" == loaded ]] || { echo "$unit is not loaded" >&2; exit 1; }
[[ "$(prop NeedDaemonReload)" == no ]] || { echo "$unit needs daemon-reload" >&2; exit 1; }
[[ -z "$(prop Job)" ]] || { echo "$unit has a pending job" >&2; exit 1; }
state="$(prop ActiveState)"
case "$action" in
  stop)
    [[ "$state" == active ]] || { echo "$unit is $state, not active" >&2; exit 1; }
    sudo -n systemctl stop "$unit"
    report
    if [[ "$(prop ActiveState)" != inactive || "$(prop MainPID)" != 0 || -n "$(prop Job)" || "$(prop Result)" != success ]]; then
      echo "$unit did not stop cleanly" >&2
      exit 1
    fi
    echo "[OUTCOME] stopped $unit"
    ;;
  start)
    [[ "$state" == inactive && "$(prop MainPID)" == 0 ]] || { echo "$unit is $state, not stopped" >&2; exit 1; }
    since="$(date -u +%s)"
    echo "[STARTED_AT] $(date -u -d "@$since" +%Y-%m-%dT%H:%M:%SZ)"
    sudo -n systemctl start "$unit"
    for _ in {1..45}; do
      [[ "$(prop ActiveState)" == active && "$(prop SubState)" == running ]] && break
      sleep 2
    done
    pid="$(prop MainPID)"
    entered="$(date -u -d "$(prop ActiveEnterTimestamp)" +%s)"
    if [[ "$(prop SubState)" != running || "$pid" == 0 || "$entered" -lt "$since" ]]; then
      report
      echo "$unit did not start" >&2
      exit 1
    fi
    sleep 60
    report
    if [[ "$(prop ActiveState)" != active || "$(prop MainPID)" != "$pid" || "$(prop NRestarts)" != 0 ]]; then
      echo "$unit did not stay running after start" >&2
      exit 1
    fi
    echo "[OUTCOME] started $unit"
    ;;
esac
REMOTE
)" || { printf '%s\n' "${output:-}"; exit 1; }
printf '%s\n' "$output"

if [[ "$slot" == a && "$action" == start ]]; then
  started_at="$(sed -n 's/^\[STARTED_AT\] //p' <<<"$output")"
  CHANGE_STARTED_AT="$started_at" "$tool_root/scripts/deploy/ap-completion-check.sh" osaka
fi
