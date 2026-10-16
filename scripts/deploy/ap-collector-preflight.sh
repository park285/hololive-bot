#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
AP_PREFLIGHT_ALLOW_FIRST_BOOT="${AP_PREFLIGHT_ALLOW_FIRST_BOOT:-false}"
AP_PREFLIGHT_ALLOW_STOPPED="${AP_PREFLIGHT_ALLOW_STOPPED:-false}"
AP_REQUIRED_UDP_BUFFER_BYTES="${AP_REQUIRED_UDP_BUFFER_BYTES:-7500000}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

case "$AP_PREFLIGHT_ALLOW_FIRST_BOOT" in
  true|false) ;;
  *)
    echo "AP_PREFLIGHT_ALLOW_FIRST_BOOT must be true or false" >&2
    exit 2
    ;;
esac
case "$AP_PREFLIGHT_ALLOW_STOPPED" in
  true|false) ;;
  *)
    echo "AP_PREFLIGHT_ALLOW_STOPPED must be true or false" >&2
    exit 2
    ;;
esac
if [[ "$AP_PREFLIGHT_ALLOW_FIRST_BOOT" == true && "$AP_PREFLIGHT_ALLOW_STOPPED" == true ]]; then
  echo "AP_PREFLIGHT_ALLOW_FIRST_BOOT and AP_PREFLIGHT_ALLOW_STOPPED are mutually exclusive" >&2
  exit 2
fi
if [[ ! "$AP_REQUIRED_UDP_BUFFER_BYTES" =~ ^[0-9]+$ ]]; then
  echo "AP_REQUIRED_UDP_BUFFER_BYTES must be an integer" >&2
  exit 2
fi

containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"
udp_buffer_check="$REPO_ROOT/scripts/deploy/lib/require-quic-udp-buffer.sh"
if [[ ! -r "$udp_buffer_check" ]]; then
  echo "QUIC UDP buffer check helper not readable: $udp_buffer_check" >&2
  exit 1
fi

remote_required_udp_buffer="$(printf '%q' "$AP_REQUIRED_UDP_BUFFER_BYTES")"
remote_ap_name="$(printf '%q' "$AP_NAME")"
"${AP_SSH[@]}" "bash -s -- $remote_required_udp_buffer $remote_ap_name" < "$udp_buffer_check"

"${AP_SSH[@]}" \
  "AP_PREFLIGHT_ALLOW_FIRST_BOOT='$AP_PREFLIGHT_ALLOW_FIRST_BOOT' AP_PREFLIGHT_ALLOW_STOPPED='$AP_PREFLIGHT_ALLOW_STOPPED' AP_CONTAINERS_LIST='$containers_list' AP_PORTS_LIST='$ports_list' AP_NAME='$AP_NAME' bash -s" <<'REMOTE'
set -euo pipefail

sudo -n test -r /etc/stack-secrets/hololive-bot/ap-compose.env
sudo -n test -r /etc/stack-secrets/hololive-bot/youtube-collector.env
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/postgres-ca.pem
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/hololive-h3.crt
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/hololive-h3.key
sudo -n openssl x509 -in /etc/stack-secrets/hololive-bot/certs/postgres-ca.pem -noout >/dev/null
sudo -n openssl x509 -in /etc/stack-secrets/hololive-bot/certs/hololive-h3.crt -noout >/dev/null
test -w /var/run/docker.sock || groups | grep -qw docker

containers=($AP_CONTAINERS_LIST)
ports=($AP_PORTS_LIST)
[[ ${#containers[@]} -eq ${#ports[@]} ]]

existing=0
missing=0
for container in "${containers[@]}"; do
  if docker inspect "$container" >/dev/null 2>&1; then
    existing=$((existing + 1))
  else
    missing=$((missing + 1))
  fi
done

if [[ "$existing" -eq 0 ]]; then
  if [[ "$AP_PREFLIGHT_ALLOW_FIRST_BOOT" == true ]]; then
    echo "$AP_NAME first boot: no AP collector containers exist yet"
    exit 0
  fi
  echo "No AP collector containers exist on $AP_NAME. Set AP_PREFLIGHT_ALLOW_FIRST_BOOT=true only for the documented first boot." >&2
  exit 1
fi
if [[ "$missing" -gt 0 ]]; then
  echo "Partial AP collector set on $AP_NAME ($missing missing); refusing preflight" >&2
  exit 1
fi

for index in "${!containers[@]}"; do
  container="${containers[$index]}"
  port="${ports[$index]}"
  if [[ "$AP_PREFLIGHT_ALLOW_STOPPED" == true ]]; then
    # 점검 창에서 정지한 collector만 받는다. 실행 중이면 점검 상태가 아니므로 일반 배포 경로를 쓴다.
    status="$(docker inspect -f '{{.State.Status}}' "$container")"
    if [[ "$status" != exited && "$status" != created ]]; then
      echo "$container is not stopped for maintenance: $status" >&2
      exit 1
    fi
    echo "$container stopped for maintenance: $status"
    continue
  fi
  status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
  if [[ "$status" != healthy ]]; then
    echo "$container is not healthy before collector deploy: $status" >&2
    exit 1
  fi
  docker exec "$container" ./bin/healthcheck "https://127.0.0.1:${port}/health"
done

echo "$AP_NAME collector preflight passed"
REMOTE
