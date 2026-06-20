#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
LOG_SINCE="${LOG_SINCE:-10m}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ ${#AP_SERVICES[@]} -ne 1 || ${#AP_PORTS[@]} -ne 1 ]]; then
  echo "host-native status currently supports exactly one AP service per host" >&2
  exit 2
fi

service="${AP_SERVICES[0]}"
port="${AP_PORTS[0]}"

ap_remote_bash "$service" "$port" "$LOG_SINCE" <<'REMOTE'
set -euo pipefail
service="$1"
port="$2"
since="$3"
unit="hololive-youtube-producer@${service}.service"

echo "== systemd =="
systemctl show "$unit" -p ActiveState -p SubState -p ExecMainPID -p MemoryCurrent -p NRestarts -p ActiveEnterTimestamp || true
printf 'net.core.rmem_max=%s\n' "$(sysctl -n net.core.rmem_max 2>/dev/null || echo unknown)"
printf 'net.core.wmem_max=%s\n' "$(sysctl -n net.core.wmem_max 2>/dev/null || echo unknown)"

echo
echo "== runtime files =="
sudo -n find /run/hololive-bot -maxdepth 3 -printf "%M %u %g %s %TY-%Tm-%TdT%TH:%TM:%TS %p\n" 2>/dev/null | sort || true

echo
echo "== env keys =="
sudo -n sh -c 'for f in /run/hololive-bot/*.env /etc/hololive-bot/youtube-producer-host.env; do test -r "$f" && printf "%s\n" "$f" && cut -d= -f1 "$f" | sort; done' || true

echo
echo "== health =="
if [[ -x /opt/hololive-bot/youtube-producer/current/bin/healthcheck ]]; then
  sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
    /opt/hololive-bot/youtube-producer/current/bin/healthcheck "https://127.0.0.1:${port}/health" || true
  sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
    /opt/hololive-bot/youtube-producer/current/bin/healthcheck --body "https://127.0.0.1:${port}/ready" || true
else
  echo "(healthcheck binary missing)"
fi

echo
echo "== recent signals =="
journalctl -u "$unit" --since "$since" --no-pager 2>/dev/null |
  grep -E 'PostgreSQL|Valkey|active_active|job_claim|ERR|panic|permission denied|x509|no such file' || true
REMOTE
