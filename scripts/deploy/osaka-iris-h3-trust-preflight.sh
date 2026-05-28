#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"

if [[ ! -r "$SSH_KEY" ]]; then
  echo "SSH key not readable: $SSH_KEY" >&2
  exit 1
fi

SSH_OSAKA=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C "ubuntu@$OSAKA_HOST")

"${SSH_OSAKA[@]}" 'bash -s' <<'REMOTE'
set -euo pipefail

cd ~/hololive-bot

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

sudo -n test -r /run/hololive-bot/env
sudo -n test -r /run/hololive-bot/certs/iris-ca.pem
sudo -n openssl x509 -in /run/hololive-bot/certs/iris-ca.pem -noout >/dev/null

iris_base_url="$(sudo -n awk -F= '$1 == "IRIS_BASE_URL" {print $2}' /run/hololive-bot/env | tail -n 1)"
iris_server_name="$(sudo -n awk -F= '$1 == "IRIS_H3_SERVER_NAME" {print $2}' /run/hololive-bot/env | tail -n 1)"
if [[ -z "$iris_base_url" ]]; then
  echo "IRIS_BASE_URL is missing from rendered env" >&2
  exit 1
fi
if [[ "$iris_base_url" != https://* ]]; then
  echo "IRIS_BASE_URL must be H3 https for Osaka trust preflight" >&2
  exit 1
fi

ready_url="${iris_base_url%/}/ready"
for container in hololive-youtube-producer-a hololive-youtube-producer-b; do
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

echo "osaka Iris H3 trust preflight passed"
REMOTE
