#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
. "$ROOT_DIR/scripts/deploy/lib/compose-env.sh"
. "$ROOT_DIR/scripts/deploy/lib/compose-services.sh"
. "$ROOT_DIR/scripts/deploy/lib/removed-runtimes.sh"
REPO_CANONICAL_ROOT="$(cd "$(git rev-parse --path-format=absolute --git-common-dir)/.." && pwd)"

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
COMPOSE_FILE_PATHS=("${COMPOSE_FILE}")
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

usage() {
    echo "Usage: $0 <service|all>"
    echo
    echo "Supported services:"
    compose_service_redeploy_usage_lines
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
if ! TARGET="$(compose_service_resolve_redeploy_target "$SERVICE")"; then
    echo "[ERROR] Unsupported service: $SERVICE"
    echo
    usage
    exit 1
fi

if [ "$TARGET" = "youtube-scraper" ] && [[ ",${COMPOSE_FILE}," != *"docker-compose.osaka.yml"* ]] && [ "${ALLOW_CENTRAL_YOUTUBE_SCRAPER:-}" != "true" ]; then
    echo "[ERROR] youtube-scraper is Osaka-owned. Refusing central redeploy without ALLOW_CENTRAL_YOUTUBE_SCRAPER=true."
    exit 1
fi
if [ -z "$TARGET" ] && [[ ",${COMPOSE_FILE}," != *"docker-compose.osaka.yml"* ]] && [[ ",${COMPOSE_PROFILES:-}," == *",oracle,"* ]] && [ "${ALLOW_CENTRAL_YOUTUBE_SCRAPER:-}" != "true" ]; then
    echo "[ERROR] COMPOSE_PROFILES=oracle would include youtube-scraper, which is Osaka-owned. Refusing central all-service deploy."
    exit 1
fi

if ! COMPOSE_ENV_FILE="$(compose_env_resolve_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE
compose_env_validate_file_format "$COMPOSE_ENV_FILE"
compose_env_assert_shell_matches_all_file_keys "$COMPOSE_ENV_FILE"
compose_env_assert_no_shell_shadow_for_compose_files "$COMPOSE_ENV_FILE" "${COMPOSE_FILE_PATHS[@]}"

export HOLO_BOT_VERSION="$(cat hololive/hololive-kakao-bot-go/VERSION 2>/dev/null | xargs || echo dev)"

echo "[INFO] COMPOSE_MODE=$COMPOSE_MODE"
echo "[INFO] COMPOSE_FILE=$COMPOSE_FILE"
echo "[INFO] HOLO_BOT_VERSION=$HOLO_BOT_VERSION"
echo "[INFO] SHARED_GO_WORKSPACE_PATH=$SHARED_GO_WORKSPACE_PATH"
echo "[INFO] COMPOSE_ENV_FILE=$COMPOSE_ENV_FILE"

if [ -n "$TARGET" ]; then
    echo "[UP] $TARGET"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" up -d --build "$TARGET"
    removed_runtime_cleanup_standalone_dispatcher
    echo "[PS] $TARGET"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" ps "$TARGET"
else
    echo "[UP] all services"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" up -d --build
    removed_runtime_cleanup_standalone_dispatcher
    echo "[PS] all services"
    "${COMPOSE_CMD[@]}" --env-file "$COMPOSE_ENV_FILE" -f "$COMPOSE_FILE" ps
fi
