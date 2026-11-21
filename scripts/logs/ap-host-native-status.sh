#!/usr/bin/env bash
set -euo pipefail

tool_script="$(realpath -e -- "${BASH_SOURCE[0]}")"
tool_root="$(cd "${tool_script%/*}/../.." && pwd -P)"
REPO_ROOT="${REPO_ROOT:-$tool_root}"
LOG_SINCE="${LOG_SINCE:-10m}"

. "$tool_root/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ ${#AP_SERVICES[@]} -ne 1 || ${#AP_PORTS[@]} -ne 1 ]]; then
  echo "host-native status currently supports exactly one AP service per host" >&2
  exit 2
fi

service="${AP_SERVICES[0]}"
port="${AP_PORTS[0]}"

{
  printf '%s\n' "$AP_SLOT_PREAMBLE"
  cat <<'REMOTE'
set -euo pipefail
service="$1"
port="$2"
since="$3"
unit="$slot_collector_unit"
current_link="$slot_root/current"

echo "== systemd =="
systemctl show "$unit" -p ActiveState -p SubState -p ExecMainPID -p MemoryCurrent -p NRestarts -p ActiveEnterTimestamp || true
printf 'net.core.rmem_max=%s\n' "$(sysctl -n net.core.rmem_max 2>/dev/null || echo unknown)"
printf 'net.core.wmem_max=%s\n' "$(sysctl -n net.core.wmem_max 2>/dev/null || echo unknown)"

echo
echo "== runtime files =="
sudo -n find /etc/stack-secrets/hololive-bot -maxdepth 3 -printf "%M %u %g %s %TY-%Tm-%TdT%TH:%TM:%TS %p\n" 2>/dev/null | sort || true

echo
echo "== env keys =="
sudo -n sh -c 'for f in /etc/stack-secrets/hololive-bot/*.env "$1"; do test -r "$f" && printf "%s\n" "$f" && cut -d= -f1 "$f" | sort; done' sh "$slot_host_env" || true

echo
echo "== health =="
if [[ -x "$current_link/bin/healthcheck" ]]; then
  sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
    "$current_link/bin/healthcheck" "https://127.0.0.1:${port}/health" || true
  sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
    "$current_link/bin/healthcheck" --body "https://127.0.0.1:${port}/ready" || true
else
  echo "(healthcheck binary missing)"
fi

echo
echo "== recent signals =="
journalctl -u "$unit" --since "$since" --no-pager 2>/dev/null |
  grep -E 'PostgreSQL|Valkey|active_active|job_claim|ERR|panic|permission denied|x509|no such file' || true
REMOTE
} | ap_remote_bash "$service" "$port" "$LOG_SINCE"
