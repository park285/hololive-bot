#!/usr/bin/env bash
set -euo pipefail
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT
ADMIN_LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ $# -ne 1 || ! "$1" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo 'usage: test-admin-bigbang-cutover.sh sha256:<local-fixture-image-id>' >&2
  exit 2
fi
ADMIN_LAB_ID="admin-lab-cutover-$$-$(date +%s)"
cleanup() {
  local id
  while IFS= read -r id; do
    if [[ "$id" =~ ^[0-9a-f]+$ ]]; then
      docker stop --time 25 "$id" >/dev/null
      docker rm "$id" >/dev/null
    fi
  done < <(docker ps -aq --filter "label=io.hololive.admin-fixture=${ADMIN_LAB_ID}")
  while IFS= read -r id; do
    [[ "$id" =~ ^[0-9a-f]+$ ]] && docker network rm "$id" >/dev/null
  done < <(docker network ls -q --filter "label=io.hololive.admin-fixture=${ADMIN_LAB_ID}")
  if [[ -d "/tmp/${ADMIN_LAB_ID}" && ! -L "/tmp/${ADMIN_LAB_ID}" && -O "/tmp/${ADMIN_LAB_ID}" ]]; then
    sudo -n chown -R "$(id -u):$(id -g)" "/tmp/${ADMIN_LAB_ID}"
    rm -r -- "/tmp/${ADMIN_LAB_ID}"
  fi
}
trap cleanup EXIT
systemd-run --user --quiet --wait --pipe --collect --unit="${ADMIN_LAB_ID}" \
  --property=RuntimeMaxSec=600 --property=KillMode=control-group --property=TimeoutStopSec=5 \
  --working-directory="${ADMIN_LAB_ROOT}" --setenv="ADMIN_FIXTURE_RUN_ID=${ADMIN_LAB_ID}" --setenv="PATH=${PATH}" \
  "$(command -v node)" scripts/deploy/test-admin-bigbang-cutover.mjs "$1"
