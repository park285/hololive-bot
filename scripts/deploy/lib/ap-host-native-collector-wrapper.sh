#!/usr/bin/env sh
set -eu

if [ -z "${POSTGRES_USER:-}" ]; then
  export POSTGRES_USER=hololive_scraper
fi
if [ "$POSTGRES_USER" != hololive_scraper ]; then
  echo "collector wrapper: POSTGRES_USER must be hololive_scraper" >&2
  exit 1
fi
if [ -z "${POSTGRES_DB:-}" ]; then
  export POSTGRES_DB=hololive
fi
# Collector는 고정 scraper 역할의 전용 자격만 사용한다.
if [ -z "${POSTGRES_PASSWORD:-}" ]; then
  if [ -z "${HOLOLIVE_SCRAPER_PASSWORD:-}" ]; then
    echo "collector wrapper: HOLOLIVE_SCRAPER_PASSWORD is required" >&2
    exit 1
  fi
  export POSTGRES_PASSWORD="$HOLOLIVE_SCRAPER_PASSWORD"
fi

if [ "$#" -eq 0 ]; then
  set -- /opt/hololive-bot/youtube-collector/current/bin/youtube-collector
fi
exec "$@"
