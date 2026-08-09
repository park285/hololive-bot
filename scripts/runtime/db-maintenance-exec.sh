#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: db-maintenance-exec.sh <command> [args...]

  중앙 런타임 호스트에서 실행합니다. 그 호스트에는 psql 이 없고 libpq-connection.sh 가
  PGPASSWORD 와 connection URI 를 금지하므로, PostgreSQL 이미지를 일회성으로 띄워
  file-only 계약(PGSERVICE + PGPASSFILE)만으로 접속합니다.
  마운트된 migrations 디렉터리는 /migrations 입니다.

  db-maintenance-exec.sh bash /migrations/preflight-durable-runtime-rollback.sh --ingress-quiesced
  db-maintenance-exec.sh bash /migrations/preflight-114-restore.sh /tmp/rollback-114.sql
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

DOCKER=(docker)
if [[ "$(id -u)" -ne 0 ]]; then
  DOCKER=(sudo -n docker)
fi

TEST=(test -e)
if [[ "$(id -u)" -ne 0 ]]; then
  TEST=(sudo -n test -e)
fi
for path in "${MIGRATIONS_DIR}" "${SECRETS_DIR}/postgres/pg_service.conf" \
            "${SECRETS_DIR}/postgres/pgpass" "${SECRETS_DIR}/certs/postgres-ca.pem"; do
  if ! "${TEST[@]}" "${path}"; then
    echo "required path is missing: ${path}" >&2
    exit 1
  fi
done

image="$("${DOCKER[@]}" inspect "${DB_CONTAINER}" -f '{{.Config.Image}}')" || {
  echo "cannot resolve image from ${DB_CONTAINER}" >&2; exit 1; }
network="$("${DOCKER[@]}" inspect "${DB_CONTAINER}" \
  -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{break}}{{end}}')" || {
  echo "cannot resolve network from ${DB_CONTAINER}" >&2; exit 1; }

exec "${DOCKER[@]}" run --rm --network "${network}" --user 0:0 \
  --read-only --tmpfs /tmp:size=64m \
  -v "${MIGRATIONS_DIR}:/migrations:ro" \
  -v "${SECRETS_DIR}/postgres:/run/hololive-bot/postgres:ro" \
  -v "${SECRETS_DIR}/certs/postgres-ca.pem:/run/hololive-bot/certs/postgres-ca.pem:ro" \
  -e PGSERVICEFILE=/run/hololive-bot/postgres/pg_service.conf \
  -e PGSERVICE="${PG_SERVICE}" \
  -e PGPASSFILE=/run/hololive-bot/postgres/pgpass \
  -e HOME=/tmp \
  "${image}" "$@"
