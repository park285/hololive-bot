#!/usr/bin/env sh
set -eu

if [ -z "${POSTGRES_USER:-}" ]; then
  export POSTGRES_USER="${HOLOLIVE_SCRAPER_USER:-hololive_scraper}"
fi
if [ -z "${POSTGRES_DB:-}" ]; then
  export POSTGRES_DB=hololive
fi
if [ -z "${POSTGRES_PASSWORD:-}" ] &&
   [ "$POSTGRES_USER" = "${HOLOLIVE_SCRAPER_USER:-hololive_scraper}" ] &&
   [ -n "${HOLOLIVE_SCRAPER_PASSWORD:-}" ]; then
  export POSTGRES_PASSWORD="$HOLOLIVE_SCRAPER_PASSWORD"
elif [ -z "${POSTGRES_PASSWORD:-}" ] && [ -n "${HOLOLIVE_DB_PASSWORD:-}" ]; then
  export POSTGRES_PASSWORD="$HOLOLIVE_DB_PASSWORD"
elif [ -z "${POSTGRES_PASSWORD:-}" ] && [ -n "${DB_PASSWORD:-}" ]; then
  export POSTGRES_PASSWORD="$DB_PASSWORD"
fi

if [ "$#" -eq 0 ]; then
  set -- /opt/hololive-bot/youtube-collector/current/bin/youtube-collector
fi
exec "$@"
