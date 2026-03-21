#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"
PRIVATE_MODULE_HOST="github.com/park285/iris-client-go"

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
    echo "  admin-dashboard | admin"
    echo "  deunhealth"
    echo "  all"
}

load_modules_token() {
    if [[ -n "${MODULES_TOKEN:-}" ]]; then
        return 0
    fi

    if [[ -n "${MODULES_TOKEN_FILE:-}" ]]; then
        if [[ ! -f "$MODULES_TOKEN_FILE" ]]; then
            echo "[ERROR] MODULES_TOKEN_FILE not found: $MODULES_TOKEN_FILE"
            exit 1
        fi

        MODULES_TOKEN="$(tr -d '\r\n' < "$MODULES_TOKEN_FILE")"
        export MODULES_TOKEN
        return 0
    fi

    if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
        MODULES_TOKEN="$(gh auth token 2>/dev/null || true)"
        export MODULES_TOKEN
    fi
}

require_modules_token() {
    if [[ -n "${MODULES_TOKEN:-}" ]]; then
        return 0
    fi

    echo "[ERROR] MODULES_TOKEN or MODULES_TOKEN_FILE is required to fetch ${PRIVATE_MODULE_HOST}"
    echo "        local fallback also checks 'gh auth token' when GitHub CLI is logged in"
    exit 1
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
    admin-dashboard|admin) TARGET="admin-dashboard" ;;
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

case "$TARGET" in
    hololive-bot|dispatcher-go|llm-scheduler|stream-ingester|youtube-scraper|"")
        load_modules_token
        require_modules_token
        ;;
esac

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
