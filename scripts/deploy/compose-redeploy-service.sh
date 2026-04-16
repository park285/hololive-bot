#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
REPO_CANONICAL_ROOT="$(cd "$(git rev-parse --path-format=absolute --git-common-dir)/.." && pwd)"

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"

resolve_shared_go_workspace_path() {
    local candidate="${SHARED_GO_WORKSPACE_PATH:-${REPO_CANONICAL_ROOT}/shared-go}"
    if [ ! -d "$candidate" ]; then
        echo "[ERROR] Active shared-go workspace not found: $candidate"
        exit 1
    fi

    printf '%s\n' "$candidate"
}

export SHARED_GO_WORKSPACE_PATH="$(resolve_shared_go_workspace_path)"

resolve_compose_env_file() {
    if [ -n "${COMPOSE_ENV_FILE:-}" ]; then
        if [ ! -f "${COMPOSE_ENV_FILE}" ]; then
            echo "[ERROR] COMPOSE_ENV_FILE not found: ${COMPOSE_ENV_FILE}"
            exit 1
        fi
        printf '%s\n' "${COMPOSE_ENV_FILE}"
        return
    fi

    local worktree_env="${ROOT_DIR}/.env"
    if [ -f "$worktree_env" ]; then
        printf '%s\n' "$worktree_env"
        return
    fi

    local canonical_env="${REPO_CANONICAL_ROOT}/.env"
    if [ -f "$canonical_env" ]; then
        printf '%s\n' "$canonical_env"
        return
    fi

    echo "[ERROR] Compose env file not found. Checked: $worktree_env, $canonical_env"
    exit 1
}

export COMPOSE_ENV_FILE="$(resolve_compose_env_file)"

usage() {
    echo "Usage: $0 <service|all>"
    echo
    echo "Supported services:"
    echo "  hololive-bot | bot"
    echo "  hololive-admin-api | admin-api"
    echo "  hololive-alarm-worker | alarm-worker"
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
    hololive-admin-api|admin-api) TARGET="hololive-admin-api" ;;
    hololive-alarm-worker|alarm-worker) TARGET="hololive-alarm-worker" ;;
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
echo "[INFO] SHARED_GO_WORKSPACE_PATH=$SHARED_GO_WORKSPACE_PATH"
echo "[INFO] COMPOSE_ENV_FILE=$COMPOSE_ENV_FILE"

if [ -n "$TARGET" ]; then
    echo "[UP] $TARGET"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" up -d --build "$TARGET"
    echo "[PS] $TARGET"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" ps "$TARGET"
else
    echo "[UP] all services"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" up -d --build
    echo "[PS] all services"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" ps
fi
