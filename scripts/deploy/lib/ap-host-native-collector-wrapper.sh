#!/usr/bin/env sh
set -eu

if [ -z "${POSTGRES_USER:-}" ]; then
  export POSTGRES_USER=hololive_collector_a
fi
if [ "$POSTGRES_USER" != hololive_collector_a ]; then
  echo "collector wrapper: POSTGRES_USER must be hololive_collector_a" >&2
  exit 1
fi
if [ -z "${POSTGRES_DB:-}" ]; then
  export POSTGRES_DB=hololive
fi
if [ -z "${POSTGRES_PASSWORD:-}" ]; then
  if [ -z "${HOLOLIVE_COLLECTOR_A_PASSWORD:-}" ]; then
    echo "collector wrapper: HOLOLIVE_COLLECTOR_A_PASSWORD is required" >&2
    exit 1
  fi
  export POSTGRES_PASSWORD="$HOLOLIVE_COLLECTOR_A_PASSWORD"
fi

if [ "$#" -eq 0 ]; then
  set -- /opt/hololive-bot/collector/current/bin/hololive-collector
fi
exec "$@"
