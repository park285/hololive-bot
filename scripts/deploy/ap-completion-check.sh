#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
CHANGE_STARTED_AT="${CHANGE_STARTED_AT:-}"
AP_REQUIRED_UDP_BUFFER_BYTES="${AP_REQUIRED_UDP_BUFFER_BYTES:-7500000}"
NODE_VERSION_LIB="$REPO_ROOT/scripts/deploy/lib/youtubejs-node-version.sh"
READINESS_LIB="$REPO_ROOT/scripts/deploy/lib/ap-collector-readiness.sh"
PO_NATIVE_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-po.sh"

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
  {
    cat "$NODE_VERSION_LIB"
    cat "$READINESS_LIB"
    cat "$PO_NATIVE_LIB"
    cat <<'REMOTE'
set -euo pipefail
service="$1"
port="$2"
change_started_at="$3"
unit="hololive-youtube-collector@${service}.service"
current_link="/opt/hololive-bot/youtube-collector/current"

sudo -n test -r /etc/stack-secrets/hololive-bot/youtube-collector.env
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/postgres-ca.pem
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/hololive-h3.crt
sudo -n test -r /etc/stack-secrets/hololive-bot/certs/hololive-h3.key
sudo -n test -r /etc/hololive-bot/youtube-collector-host.env
sudo -n test -x "$current_link/bin/healthcheck"
sudo -n test -f "$current_link/youtubejs/src/server.mjs"
require_node_version node

po_validate_release "$current_link"
systemctl is-active --quiet hololive-youtube-po.socket
systemctl is-active --quiet hololive-youtube-po.service
sudo -n -u hololive "$current_link/po-sandbox/rootfs/app/bin/po-broker" --healthcheck --socket /run/hololive-youtube-po/worker.sock
python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); assert (m["source_revision"],m["version"],m["go"]["goarch"]) == (open(sys.argv[2]).read().strip(),open(sys.argv[3]).read().strip(),"amd64")' \
  "$current_link/manifest.json" "$current_link/po-sandbox/revision" "$current_link/po-sandbox/version"
systemctl is-active --quiet "$unit"
active_state="$(systemctl show "$unit" -p ActiveState --value)"
sub_state="$(systemctl show "$unit" -p SubState --value)"
restart_count="$(systemctl show "$unit" -p NRestarts --value)"
environment_files="$(systemctl show "$unit" -p EnvironmentFiles --value)"
[[ "$active_state" == active ]]
[[ "$sub_state" == running ]]
[[ "$restart_count" == 0 ]]
[[ "$environment_files" == *'/etc/stack-secrets/hololive-bot/youtube-collector.env'* ]]
[[ "$environment_files" == *'/etc/hololive-bot/youtube-collector-host.env'* ]]
[[ "$environment_files" != *'compose.env'* ]]

if [[ -n "$change_started_at" ]]; then
  since_epoch="$(date -u -d "$change_started_at" +%s)"
  active_enter="$(systemctl show "$unit" -p ActiveEnterTimestamp --value)"
  active_epoch="$(date -u -d "$active_enter" +%s)"
  [[ "$active_epoch" -ge "$since_epoch" ]]
fi

sudo -n grep -qx 'YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true' /etc/hololive-bot/youtube-collector-host.env
sudo -n grep -qx 'POSTGRES_USER=hololive_scraper' /etc/hololive-bot/youtube-collector-host.env

sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
  "$current_link/bin/healthcheck" "https://127.0.0.1:${port}/health" >/dev/null
ready="$(
  sudo -n -u hololive env \
  HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt \
  HEALTHCHECK_SERVER_NAME=127.0.0.1 \
  "$current_link/bin/healthcheck" --body "https://127.0.0.1:${port}/ready"
)"
printf '%s\n' "$ready"
collector_readiness_validate "$ready"

if [[ -n "$change_started_at" ]]; then
  journal_since="${change_started_at/T/ }"
  journal_since="${journal_since%Z} UTC"
else
  journal_since="10 minutes ago"
fi
if ! journal_output="$(journalctl -u "$unit" --since "$journal_since" --no-pager)"; then
  echo "failed to read completion logs for $unit" >&2
  exit 1
fi
if grep -E 'ERR|panic|permission denied|x509|no such file' <<<"$journal_output"; then
  exit 1
fi

echo 'collector AP completion check passed'
REMOTE
  } | ap_remote_bash "$service" "$port" "$CHANGE_STARTED_AT"
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
. scripts/deploy/lib/youtubejs-node-version.sh
. scripts/deploy/lib/ap-collector-readiness.sh
bash scripts/deploy/lib/require-quic-udp-buffer.sh '$AP_REQUIRED_UDP_BUFFER_BYTES' '$AP_NAME'
sudo -n test -r /etc/stack-secrets/hololive-bot/ap-compose.env
sudo -n test -r /etc/stack-secrets/hololive-bot/youtube-collector.env
test -w /var/run/docker.sock || groups | grep -qw docker
. scripts/deploy/lib/po-sandbox-image.sh
po_manifest=backups/po-sandbox-current-b.json
test -r \"\$po_manifest\"
po_revision=\$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[\"source_revision\"])' \"\$po_manifest\")
[[ \"\$po_revision\" =~ ^[0-9a-f]{40}\$ ]]
po_verify_image hololive-youtube-po-sandbox:prod \"\$po_manifest\" \"\$po_revision\" arm64 backups/po-sandbox-current-b.image-id
[[ \$(docker inspect -f '{{index .Config.Labels \"org.opencontainers.image.revision\"}}' hololive-youtube-po-b) == \"\$po_revision\" ]]
po_version=\$(sudo -n docker image inspect -f '{{index .Config.Labels \"org.opencontainers.image.version\"}}' hololive-youtube-po-sandbox:prod)
[[ \"\$po_version\" == \$(cat hololive/hololive-api/VERSION) ]]
[[ \$(docker inspect -f '{{.Image}}' hololive-youtube-po-b) == \$(sudo -n docker image inspect -f '{{.Id}}' hololive-youtube-po-sandbox:prod) ]]
[[ \$(docker inspect -f '{{.HostConfig.NetworkMode}}' hololive-youtube-po-b) == none ]]
[[ \$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' hololive-youtube-po-b) == true ]]
[[ \$(docker inspect -f '{{.Config.User}}' hololive-youtube-po-b) == '65532:1000' ]]
socket_mount=\$(docker inspect -f '{{range .Mounts}}{{if eq .Destination \"/run/hololive-youtube-po\"}}{{.Source}}{{end}}{{end}}' hololive-youtube-po-b)
[[ \$(sudo -n stat -c '%u:%g %a' \"\$socket_mount\") == '65532:1000 770' ]]
docker exec hololive-youtube-po-b /app/bin/po-broker --healthcheck --socket /run/hololive-youtube-po/worker.sock
sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f '$AP_COMPOSE_FILE' ps $services_list

for container in $containers_list; do
  docker inspect \"\$container\" >/dev/null
  node_version=\$(docker exec \"\$container\" node --version)
  node_version_supported \"\$node_version\"
  status=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \"\$container\")
  [[ \"\$status\" == healthy ]]
  [[ \$(docker inspect -f '{{index .Config.Labels \"org.opencontainers.image.revision\"}}' \"\$container\") == \"\$po_revision\" ]]
  [[ \$(docker inspect -f '{{index .Config.Labels \"org.opencontainers.image.version\"}}' \"\$container\") == \"\$po_version\" ]]
done

ports=($ports_list)
idx=0
for container in $containers_list; do
  ready=\$(docker exec \"\$container\" ./bin/healthcheck --body \"https://127.0.0.1:\${ports[\$idx]}/ready\")
  collector_readiness_validate \"\$ready\"
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' \"\$container\" | grep -qx 'YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true'
  docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' \"\$container\" | grep -qx 'POSTGRES_USER=hololive_scraper'
  idx=\$((idx + 1))
done

if [[ -n '$CHANGE_STARTED_AT' ]]; then
  since_epoch=\$(date -u -d '$CHANGE_STARTED_AT' +%s)
  po_started=\$(docker inspect -f '{{.State.StartedAt}}' hololive-youtube-po-b)
  [[ \$(date -u -d \"\$po_started\" +%s) -ge \"\$since_epoch\" ]]
  for container in $containers_list; do
    started_at=\$(docker inspect -f '{{.State.StartedAt}}' \"\$container\")
    started_epoch=\$(date -u -d \"\$started_at\" +%s)
    [[ \"\$started_epoch\" -ge \"\$since_epoch\" ]]
    if docker logs --since '$CHANGE_STARTED_AT' \"\$container\" 2>&1 | grep -E 'ERR|panic|permission denied|x509|no such file'; then
      exit 1
    fi
  done
fi

echo 'collector AP completion check passed'
"
