#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"

usage() {
    echo "Usage: $0 <service|all>"
    echo
    echo "Supported services:"
    echo "  hololive-bot | bot"
    echo "  dispatcher-go | dispatcher"
    echo "  llm-scheduler | llm"
    echo "  stream-ingester | ingester"
    echo "  youtube-scraper | yt-scraper"
    echo "  holo-postgres | postgres"
    echo "  valkey-cache | valkey"
    echo "  hololive-db-migrate | migrate"
    echo "  docker-proxy"
    echo "  deunhealth"
    echo "  all"
}

if [ $# -ne 1 ]; then
    usage
    exit 1
fi

case "$CONTAINER_CLI" in
    docker|podman) ;;
    *)
        echo "[ERROR] Unsupported CONTAINER_CLI: $CONTAINER_CLI"
        echo "        Allowed values: docker, podman"
        exit 1
        ;;
esac

if ! command -v "$CONTAINER_CLI" >/dev/null 2>&1; then
    echo "[ERROR] Container CLI not found: $CONTAINER_CLI"
    exit 1
fi

COMPOSE_CMD=("$CONTAINER_CLI" "compose")
COMPOSE_MODE="$CONTAINER_CLI compose"
if [ "$CONTAINER_CLI" = "podman" ] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=("podman-compose")
    COMPOSE_MODE="podman-compose"
elif ! "$CONTAINER_CLI" compose version >/dev/null 2>&1; then
    if [ "$CONTAINER_CLI" = "podman" ] && command -v podman-compose >/dev/null 2>&1; then
        COMPOSE_CMD=("podman-compose")
        COMPOSE_MODE="podman-compose"
    else
        echo "[ERROR] '$CONTAINER_CLI compose' is unavailable"
        exit 1
    fi
fi

SERVICE="$1"
case "$SERVICE" in
    hololive-bot|bot) TARGET="hololive-bot" ;;
    dispatcher-go|dispatcher) TARGET="dispatcher-go" ;;
    llm-scheduler|llm) TARGET="llm-scheduler" ;;
    stream-ingester|ingester) TARGET="stream-ingester" ;;
    youtube-scraper|yt-scraper) TARGET="youtube-scraper" ;;
    holo-postgres|postgres) TARGET="holo-postgres" ;;
    valkey-cache|valkey) TARGET="valkey-cache" ;;
    hololive-db-migrate|migrate) TARGET="hololive-db-migrate" ;;
    docker-proxy) TARGET="docker-proxy" ;;
    deunhealth) TARGET="deunhealth" ;;
    all) TARGET="" ;;
    *)
        echo "[ERROR] Unsupported service: $SERVICE"
        echo
        usage
        exit 1
        ;;
esac

export HOLO_BOT_VERSION="$(cat hololive/hololive-kakao-bot-go/VERSION 2>/dev/null | xargs || echo dev)"

echo "[INFO] COMPOSE_MODE=$COMPOSE_MODE"
echo "[INFO] COMPOSE_FILE=$COMPOSE_FILE"
echo "[INFO] HOLO_BOT_VERSION=$HOLO_BOT_VERSION"

if [ -n "$TARGET" ]; then
    echo "[UP] $TARGET"
    "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" up -d --build "$TARGET"
    echo "[PS] $TARGET"
    "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" ps "$TARGET"
else
    echo "[UP] all services"
    "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" up -d --build
    echo "[PS] all services"
    "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" ps
fi
