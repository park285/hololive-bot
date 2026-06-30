#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
CHANGE_STARTED_AT="${CHANGE_STARTED_AT:-}"
AP_REQUIRED_UDP_BUFFER_BYTES="${AP_REQUIRED_UDP_BUFFER_BYTES:-7500000}"

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ -n "$CHANGE_STARTED_AT" && ! "$CHANGE_STARTED_AT" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
  echo "CHANGE_STARTED_AT must be UTC RFC3339 seconds, for example 2026-05-19T18:28:03Z" >&2
  exit 2
fi
if [[ ! "$AP_REQUIRED_UDP_BUFFER_BYTES" =~ ^[0-9]+$ ]]; then
  echo "AP_REQUIRED_UDP_BUFFER_BYTES must be an integer" >&2
  exit 2
fi

remote() {
  "${AP_SSH[@]}" "$@"
}

run_native_completion_check() {
  if [[ ${#AP_SERVICES[@]} -ne 1 || ${#AP_PORTS[@]} -ne 1 ]]; then
    echo "host-native completion check currently supports exactly one AP service per host" >&2
    exit 2
  fi

  local service="${AP_SERVICES[0]}"
  local port="${AP_PORTS[0]}"

  ap_remote_bash "$AP_REQUIRED_UDP_BUFFER_BYTES" "$AP_NAME" < "$REPO_ROOT/scripts/deploy/lib/require-quic-udp-buffer.sh"
  ap_remote_bash "$service" "$port" "$CHANGE_STARTED_AT" <<'REMOTE'
set -euo pipefail
service="$1"
port="$2"
change_started_at="$3"
unit="hololive-youtube-producer@${service}.service"
current_link="/opt/hololive-bot/youtube-producer/current"

sudo -n test -r /run/hololive-bot/ap-compose.env
sudo -n test -r /run/hololive-bot/youtube-producer.env
sudo -n test -r /run/hololive-bot/certs/postgres-ca.pem
sudo -n test -r /run/hololive-bot/certs/hololive-h3.crt
sudo -n test -r /run/hololive-bot/certs/hololive-h3.key
sudo -n test -r /etc/hololive-bot/youtube-producer-host.env
sudo -n test -d /var/lib/hololive-bot/youtube-producer/settings
sudo -n test -x "$current_link/bin/healthcheck"

systemctl is-active --quiet "$unit"
active_state="$(systemctl show "$unit" -p ActiveState --value)"
sub_state="$(systemctl show "$unit" -p SubState --value)"
restart_count="$(systemctl show "$unit" -p NRestarts --value)"
[[ "$active_state" == active ]]
[[ "$sub_state" == running ]]
[[ "$restart_count" == 0 ]]

if [[ -n "$change_started_at" ]]; then
  since_epoch="$(date -u -d "$change_started_at" +%s)"
  active_enter="$(systemctl show "$unit" -p ActiveEnterTimestamp --value)"
  active_epoch="$(date -u -d "$active_enter" +%s)"
  [[ "$active_epoch" -ge "$since_epoch" ]]
fi

sudo -n grep -qx 'YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true' /etc/hololive-bot/youtube-producer-host.env
sudo -n grep -qx 'SETTINGS_DIR=/var/lib/hololive-bot/youtube-producer/settings' /etc/hololive-bot/youtube-producer-host.env
sudo -n -u hololive test -w /var/lib/hololive-bot/youtube-producer/settings

sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
  "$current_link/bin/healthcheck" "https://127.0.0.1:${port}/health" >/dev/null
ready="$(
  sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/run/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
  "$current_link/bin/healthcheck" --body "https://127.0.0.1:${port}/ready"
)"
printf '%s\n' "$ready"
printf '%s' "$ready" | grep -q '"status":"ready"'

if [[ -n "$change_started_at" ]]; then
  journal_since="${change_started_at/T/ }"
  journal_since="${journal_since%Z} UTC"
else
  journal_since="10 minutes ago"
fi
if journalctl -u "$unit" --since "$journal_since" --no-pager |
   grep -E 'ERR|panic|permission denied|x509|no such file|ingestion_lease_lost'; then
  exit 1
fi

echo 'active-active completion check passed'
REMOTE
}

if [[ "${AP_RUNTIME_MODE:-compose}" == "native" ]]; then
  run_native_completion_check
  exit 0
fi

services_list="${AP_SERVICES[*]}"
containers_list="${AP_CONTAINERS[*]}"
ports_list="${AP_PORTS[*]}"

remote "set -euo pipefail
cd ~/hololive-bot
bash scripts/deploy/lib/require-quic-udp-buffer.sh '$AP_REQUIRED_UDP_BUFFER_BYTES' '$AP_NAME'
sudo -n test -r /run/hololive-bot/ap-compose.env
sudo -n test -r /run/hololive-bot/youtube-producer.env
test -w /var/run/docker.sock || groups | grep -qw docker
sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f '$AP_COMPOSE_FILE' ps $services_list

for container in $containers_list; do
  docker inspect \"\$container\" >/dev/null
  status=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \"\$container\")
  [[ \"\$status\" == healthy ]]
done

ports=($ports_list)
idx=0
for container in $containers_list; do
  ready=\$(docker exec \"\$container\" ./bin/healthcheck --body \"https://127.0.0.1:\${ports[\$idx]}/ready\")
  printf '%s' \"\$ready\" | grep -q '\"status\":\"ready\"'
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' \"\$container\" | grep -qx 'YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED=true'
  idx=\$((idx + 1))
done

if [[ -n '$CHANGE_STARTED_AT' ]]; then
  since_epoch=\$(date -u -d '$CHANGE_STARTED_AT' +%s)
  for container in $containers_list; do
    started_at=\$(docker inspect -f '{{.State.StartedAt}}' \"\$container\")
    started_epoch=\$(date -u -d \"\$started_at\" +%s)
    [[ \"\$started_epoch\" -ge \"\$since_epoch\" ]]
    if docker logs --since '$CHANGE_STARTED_AT' \"\$container\" 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'; then
      exit 1
    fi
  done
fi

echo 'active-active completion check passed'
"
