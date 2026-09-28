#!/usr/bin/env sh
set -eu

if [ -z "${POSTGRES_USER:-}" ]; then
  export POSTGRES_USER="${HOLOLIVE_SCRAPER_USER:-hololive_scraper}"
fi
if [ -z "${POSTGRES_DB:-}" ]; then
  export POSTGRES_DB=hololive
fi
# 역할마다 자기 비밀번호 하나만 쓴다. scraper 역할이 runtime 비밀번호나 admin DB_PASSWORD로 내려가는
# 폴백 체인은 두지 않는다(stack audit 2026-09-26, T18에서 모든 collector env에 HOLOLIVE_SCRAPER_PASSWORD 확인).
if [ -z "${POSTGRES_PASSWORD:-}" ]; then
  if [ "$POSTGRES_USER" = "${HOLOLIVE_SCRAPER_USER:-hololive_scraper}" ]; then
    role_password_key=HOLOLIVE_SCRAPER_PASSWORD
    role_password="${HOLOLIVE_SCRAPER_PASSWORD:-}"
  else
    role_password_key=HOLOLIVE_DB_PASSWORD
    role_password="${HOLOLIVE_DB_PASSWORD:-}"
  fi

  if [ -z "$role_password" ]; then
    echo "collector wrapper: ${role_password_key} is required for POSTGRES_USER=${POSTGRES_USER}" >&2
    exit 1
  fi

  export POSTGRES_PASSWORD="$role_password"
fi

if [ "$#" -eq 0 ]; then
  set -- /opt/hololive-bot/youtube-collector/current/bin/youtube-collector
fi
exec "$@"
