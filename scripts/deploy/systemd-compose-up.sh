#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${HOLOLIVE_BOT_ROOT:-/home/kapu/work/iris-stack/hololive-bot}"
cd "$ROOT_DIR"

wait_for_tailscale_ip() {
  for _ in $(seq 1 90); do
    if ip -4 addr show tailscale0 2>/dev/null | grep -q '100\.100\.1\.3/32'; then
      return 0
    fi
    sleep 1
  done
  echo "tailscale0 missing 100.100.1.3/32 after 90s" >&2
  return 1
}

wait_for_file() {
  local path="$1"

  for _ in $(seq 1 90); do
    if [ -f "$path" ] && [ -s "$path" ]; then
      return 0
    fi
    if [ -d "$path" ]; then
      echo "$path is a directory; OpenBao render did not complete cleanly" >&2
      return 1
    fi
    sleep 1
  done
  echo "$path was not rendered after 90s" >&2
  return 1
}

wait_for_tailscale_ip
for file in \
  /run/hololive-bot/compose.env \
  /run/hololive-bot/bot.env \
  /run/hololive-bot/alarm-worker.env \
  /run/hololive-bot/youtube-producer.env \
  /run/hololive-bot/certs/hololive-h3.crt \
  /run/hololive-bot/certs/hololive-h3.key \
  /run/hololive-bot/certs/iris-ca.pem \
  /run/hololive-bot/certs/postgres-ca.pem \
  /run/hololive-bot/postgres-tls/server.crt \
  /run/hololive-bot/postgres-tls/server.key
do
  wait_for_file "$file"
done

export COMPOSE_ENV_FILE=/run/hololive-bot/compose.env

./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d

COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  -f deploy/compose/docker-compose.main-ap.yml \
  -f deploy/compose/docker-compose.main-ap.live-compat.yml \
  up -d youtube-producer-c
