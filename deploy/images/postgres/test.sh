#!/bin/sh
set -eu

if [ "$(id -u)" = 0 ]; then
  [ "$(gosu 999:999 id -u)" = 999 ]
  [ "$(gosu 999:999 id -g)" = 999 ]
else
  [ "$(id -u)" = 999 ]
fi

docker-entrypoint.sh postgres -c listen_addresses= &
server=$!
trap 'kill -TERM "$server" 2>/dev/null || true; wait "$server" || true' EXIT
attempts=0
while :; do
  kill -0 "$server"
  read -r command < "/proc/$server/comm"
  # initdb의 임시 서버가 아니라 entrypoint가 exec한 최종 서버를 검증한다.
  if [ "$command" = postgres ] && pg_isready -h /var/run/postgresql -U postgres >/dev/null 2>&1; then
    break
  fi
  attempts=$((attempts + 1))
  [ "$attempts" -lt 30 ]
  sleep 1
done
[ "$(psql -h /var/run/postgresql -U postgres -Atqc 'SELECT current_user')" = postgres ]
[ "$(psql -h /var/run/postgresql -U postgres -Atqc 'SHOW server_version_num')" = 180006 ]
printf 'PostgreSQL startup and SQL passed (initial uid=%s)\n' "$(id -u)"
