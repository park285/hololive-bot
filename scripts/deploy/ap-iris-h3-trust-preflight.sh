#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
AP_PREFLIGHT_ALLOW_FIRST_BOOT="${AP_PREFLIGHT_ALLOW_FIRST_BOOT:-false}"
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
if [[ ! "$AP_REQUIRED_UDP_BUFFER_BYTES" =~ ^[0-9]+$ ]]; then
  echo "AP_REQUIRED_UDP_BUFFER_BYTES must be an integer" >&2
  exit 2
fi

containers_list="${AP_CONTAINERS[*]}"

"${AP_SSH[@]}" "AP_PREFLIGHT_ALLOW_FIRST_BOOT='$AP_PREFLIGHT_ALLOW_FIRST_BOOT' AP_REQUIRED_UDP_BUFFER_BYTES='$AP_REQUIRED_UDP_BUFFER_BYTES' AP_CONTAINERS_LIST='$containers_list' AP_NAME='$AP_NAME' bash -s" <<'REMOTE'
set -euo pipefail

required_udp_buffer_bytes="$AP_REQUIRED_UDP_BUFFER_BYTES"
rmem_max="$(sysctl -n net.core.rmem_max 2>/dev/null || echo 0)"
wmem_max="$(sysctl -n net.core.wmem_max 2>/dev/null || echo 0)"
if (( rmem_max < required_udp_buffer_bytes || wmem_max < required_udp_buffer_bytes )); then
  echo "AP QUIC UDP buffers too small on $AP_NAME: net.core.rmem_max=$rmem_max net.core.wmem_max=$wmem_max required>=$required_udp_buffer_bytes" >&2
  exit 1
fi

unit_type="$(systemctl show openbao-agent-hololive-bot.service -p Type --value)"
unit_state="$(systemctl show openbao-agent-hololive-bot.service -p ActiveState --value)"
unit_exec="$(systemctl show openbao-agent-hololive-bot.service -p ExecStart --value)"

if [[ "$unit_type" != "simple" ]]; then
  echo "OpenBao hololive agent must run as a continuous daemon: Type=$unit_type" >&2
  exit 1
fi
if [[ "$unit_state" != "active" ]]; then
  echo "OpenBao hololive agent is not active: ActiveState=$unit_state" >&2
  exit 1
fi
if [[ "$unit_exec" == *"-exit-after-auth"* ]]; then
  echo "OpenBao hololive agent still uses -exit-after-auth" >&2
  exit 1
fi

sudo -n test -r /run/hololive-bot/ap-compose.env
sudo -n test -r /run/hololive-bot/youtube-producer.env
sudo -n test -r /run/hololive-bot/certs/iris-ca.pem
sudo -n openssl x509 -in /run/hololive-bot/certs/iris-ca.pem -noout >/dev/null

iris_base_url="$(sudo -n awk -F= '$1 == "IRIS_BASE_URL" {print $2}' /run/hololive-bot/ap-compose.env | tail -n 1)"
iris_server_name="$(sudo -n awk -F= '$1 == "IRIS_H3_SERVER_NAME" {print $2}' /run/hololive-bot/ap-compose.env | tail -n 1)"
if [[ -z "$iris_base_url" ]]; then
  echo "IRIS_BASE_URL is missing from rendered env" >&2
  exit 1
fi
if [[ "$iris_base_url" != https://* ]]; then
  echo "IRIS_BASE_URL must be H3 https for $AP_NAME trust preflight" >&2
  exit 1
fi

existing=0
missing=0
for container in $AP_CONTAINERS_LIST; do
  if docker inspect "$container" >/dev/null 2>&1; then
    existing=$((existing + 1))
  else
    missing=$((missing + 1))
  fi
done

if [[ "$existing" -eq 0 ]]; then
  if [[ "$AP_PREFLIGHT_ALLOW_FIRST_BOOT" == "true" ]]; then
    echo "$AP_NAME first boot: no AP containers exist yet; skipping in-container Iris H3 trust check"
    exit 0
  fi
  echo "No AP containers exist on $AP_NAME. Set AP_PREFLIGHT_ALLOW_FIRST_BOOT=true only for the documented first boot." >&2
  exit 1
fi

if [[ "$missing" -gt 0 ]]; then
  echo "Partial AP container set on $AP_NAME ($missing missing); refusing preflight" >&2
  exit 1
fi

ready_url="${iris_base_url%/}/ready"
for container in $AP_CONTAINERS_LIST; do
  status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
  if [[ "$status" != "healthy" ]]; then
    echo "$container is not healthy before Iris H3 trust preflight: $status" >&2
    exit 1
  fi
  docker exec \
    -e HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/iris-ca.pem \
    -e HEALTHCHECK_SERVER_NAME="$iris_server_name" \
    "$container" ./bin/healthcheck "$ready_url"
done

echo "$AP_NAME Iris H3 trust preflight passed"
REMOTE
