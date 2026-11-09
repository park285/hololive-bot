#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: db-maintenance-exec.sh <command> [args...]

  중앙 런타임 호스트(rootful Podman)에서 실행합니다. 그 호스트에는 psql 이 없고 libpq-connection.sh 가
  PGPASSWORD 와 connection URI 를 금지하므로, 실행 중인 holo-postgres 이미지를 일회성으로 띄워
  file-only 계약(PGSERVICE + PGPASSFILE)만으로 접속합니다. pg_hba 는 hololive_migrator 의 TCP 를
  거부하므로 hololive-db-migrate 처럼 network 없이 관리 Unix socket 으로만 접속합니다.
  마운트된 migrations 디렉터리는 /migrations 입니다.

  db-maintenance-exec.sh psql -w -X -v ON_ERROR_STOP=1 -c 'select 1'
EOF
  exit 2
}

(( $# > 0 )) || usage

DB_CONTAINER="${DB_CONTAINER:-holo-postgres}"
OPT_CURRENT="${OPT_CURRENT:-/opt/hololive-bot/compose/current}"
SECRETS_DIR="${SECRETS_DIR:-/etc/stack-secrets/hololive-bot}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-${OPT_CURRENT}/hololive/hololive-api/scripts/migrations}"
PG_SERVICE="${PG_SERVICE:-hololive-db-maintenance}"

# 중앙 컨테이너는 rootful Podman이 소유한다. 비root 실행은 sudo로 같은 store를 쓴다.
PODMAN=(podman)
if [[ "$(id -u)" -ne 0 ]]; then
  PODMAN=(sudo -n podman)
fi

TEST=(test -e)
if [[ "$(id -u)" -ne 0 ]]; then
  TEST=(sudo -n test -e)
fi
for path in "${MIGRATIONS_DIR}" "${SECRETS_DIR}/postgres/pg_service.conf" \
            "${SECRETS_DIR}/postgres/pgpass"; do
  if ! "${TEST[@]}" "${path}"; then
    echo "required path is missing: ${path}" >&2
    exit 1
  fi
done

# Podman .Image는 실행 중인 container의 image ID다. 같은 image를 tag 변경과 무관하게 쓴다.
image="$("${PODMAN[@]}" inspect "${DB_CONTAINER}" -f '{{.Image}}')" || {
  echo "cannot resolve image from ${DB_CONTAINER}" >&2; exit 1; }
socket_volume="$("${PODMAN[@]}" inspect "${DB_CONTAINER}" \
  -f '{{range .Mounts}}{{if and (eq .Type "volume") (eq .Destination "/var/run/postgresql")}}{{.Name}}{{end}}{{end}}')" || {
  echo "cannot resolve admin socket volume from ${DB_CONTAINER}" >&2; exit 1; }
[[ -n "${image}" && -n "${socket_volume}" ]] || { echo "cannot resolve image/admin socket volume from ${DB_CONTAINER}" >&2; exit 1; }

# bind 원본의 SELinux label은 rhel-host-security fcontext가 소유하므로 :z/:Z로 relabel하지 않는다.
exec "${PODMAN[@]}" run --rm --pull=never --network none --user 0:0 \
  --read-only --tmpfs /tmp:size=64m \
  -v "${MIGRATIONS_DIR}:/migrations:ro" \
  -v "${SECRETS_DIR}/postgres:/run/hololive-bot/postgres:ro" \
  -v "${socket_volume}:/var/run/postgresql:ro" \
  -e PGSERVICEFILE=/run/hololive-bot/postgres/pg_service.conf \
  -e PGSERVICE="${PG_SERVICE}" \
  -e PGPASSFILE=/run/hololive-bot/postgres/pgpass \
  -e HOME=/tmp \
  "${image}" "$@"
