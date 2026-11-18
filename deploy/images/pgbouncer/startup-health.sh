#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
    echo 'usage: pgbouncer-startup-health <frontend-host> <backend-host>' >&2
    exit 2
fi

for host in "$@"; do
    if ! nslookup -type=a "${host%.}."; then
        printf 'PgBouncer DNS is not ready: %s\n' "$host" >&2
        exit 1
    fi
done

exec psql -X -w -h /tmp -p 6432 -U pgbouncer -d pgbouncer \
    -v ON_ERROR_STOP=1 -Atc 'SHOW VERSION'
