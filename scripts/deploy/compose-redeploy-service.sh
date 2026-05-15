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
        echo "[ERROR] Active shared-go workspace not found: $candidate" >&2
        exit 1
    fi

    printf '%s\n' "$candidate"
}

if ! SHARED_GO_WORKSPACE_PATH="$(resolve_shared_go_workspace_path)"; then
    exit 1
fi
export SHARED_GO_WORKSPACE_PATH

resolve_compose_env_file() {
    if [ -n "${COMPOSE_ENV_FILE:-}" ]; then
        if [ ! -r "${COMPOSE_ENV_FILE}" ]; then
            echo "[ERROR] COMPOSE_ENV_FILE not readable: ${COMPOSE_ENV_FILE}" >&2
            exit 1
        fi
        printf '%s\n' "${COMPOSE_ENV_FILE}"
        return
    fi

    local openbao_env="${OPENBAO_HOLOLIVE_ENV_FILE:-/run/hololive-bot/env}"
    if [ -r "$openbao_env" ]; then
        printf '%s\n' "$openbao_env"
        return
    fi

    echo "[ERROR] Compose env file not readable. Checked: $openbao_env" >&2
    echo "        Set COMPOSE_ENV_FILE explicitly for non-OpenBao or test deployments." >&2
    exit 1
}

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

if [ "$TARGET" = "youtube-scraper" ] && [[ ",${COMPOSE_FILE}," != *"docker-compose.osaka.yml"* ]] && [ "${ALLOW_CENTRAL_YOUTUBE_SCRAPER:-}" != "true" ]; then
    echo "[ERROR] youtube-scraper is Osaka-owned. Refusing central redeploy without ALLOW_CENTRAL_YOUTUBE_SCRAPER=true."
    exit 1
fi
if [ -z "$TARGET" ] && [[ ",${COMPOSE_FILE}," != *"docker-compose.osaka.yml"* ]] && [[ ",${COMPOSE_PROFILES:-}," == *",oracle,"* ]] && [ "${ALLOW_CENTRAL_YOUTUBE_SCRAPER:-}" != "true" ]; then
    echo "[ERROR] COMPOSE_PROFILES=oracle would include youtube-scraper, which is Osaka-owned. Refusing central all-service deploy."
    exit 1
fi

if ! COMPOSE_ENV_FILE="$(resolve_compose_env_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE

export_line="$(awk '/^[[:space:]]*export[[:space:]]+/ { print NR; exit }' "$COMPOSE_ENV_FILE")"
if [ -n "$export_line" ]; then
    echo "[ERROR] Compose env file must not contain leading export: $COMPOSE_ENV_FILE:$export_line" >&2
    exit 1
fi

export HOLO_BOT_VERSION="$(cat hololive/hololive-kakao-bot-go/VERSION 2>/dev/null | xargs || echo dev)"

echo "[INFO] COMPOSE_MODE=$COMPOSE_MODE"
echo "[INFO] COMPOSE_FILE=$COMPOSE_FILE"
echo "[INFO] HOLO_BOT_VERSION=$HOLO_BOT_VERSION"
echo "[INFO] SHARED_GO_WORKSPACE_PATH=$SHARED_GO_WORKSPACE_PATH"
echo "[INFO] COMPOSE_ENV_FILE=$COMPOSE_ENV_FILE"

stop_legacy_dispatcher_after_default_deploy() {
    if [[ ",${COMPOSE_PROFILES:-}," == *",legacy-dispatcher-go,"* ]]; then
        return 0
    fi

    local container_id
    container_id="$("$CONTAINER_CLI" ps -aq --filter "name=^hololive-dispatcher-go$" 2>/dev/null || true)"
    if [[ -z "$container_id" ]]; then
        return 0
    fi

    echo "[CLEANUP] Stopping legacy dispatcher-go outside the default production profile"
    "$CONTAINER_CLI" stop hololive-dispatcher-go >/dev/null 2>&1 || true
    "$CONTAINER_CLI" rm -f hololive-dispatcher-go >/dev/null
}

if [ -n "$TARGET" ]; then
    echo "[UP] $TARGET"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" up -d --build "$TARGET"
    echo "[PS] $TARGET"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" ps "$TARGET"
else
    echo "[UP] all services"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" up -d --build
    stop_legacy_dispatcher_after_default_deploy
    echo "[PS] all services"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" ps
fi
