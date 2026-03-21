#!/bin/bash
# build-all.sh: Hololive Bot 서비스 버전 관리 및 Docker 이미지 빌드 스크립트
#
# 사용법:
#   ./build-all.sh                      # 모든 서비스 버전 bump + 빌드/배포
#   ./build-all.sh --no-bump            # 버전 bump 없이 빌드/배포
#   ./build-all.sh --build-only         # 빌드만 수행 (배포/재기동 없음)
#   ./build-all.sh --remote-cache       # registry-backed remote cache 사용 (REMOTE_CACHE_PREFIX 필요)
#   ./build-all.sh hololive-bot         # 특정 서비스만 빌드

set -e

# 스크립트 위치 기준 절대 경로로 이동 (루트에 위치)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 컨테이너 런타임 CLI (docker / podman)
CONTAINER_CLI="${CONTAINER_CLI:-docker}"
case "${CONTAINER_CLI}" in
    docker|podman) ;;
    *)
        echo "[ERROR] Unsupported CONTAINER_CLI: ${CONTAINER_CLI}"
        echo "        Allowed values: docker, podman"
        exit 1
        ;;
esac
if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
    echo "[ERROR] Container CLI not found: ${CONTAINER_CLI}"
    echo "        Set CONTAINER_CLI=docker or CONTAINER_CLI=podman"
    exit 1
fi

COMPOSE_CMD=("${CONTAINER_CLI}" "compose")
COMPOSE_MODE="${CONTAINER_CLI} compose"
PRIVATE_MODULE_HOST="github.com/park285/iris-client-go"
# podman 선택 시 docker-compose provider 의존을 피하기 위해 podman-compose 우선
if [ "${CONTAINER_CLI}" = "podman" ] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=("podman-compose")
    COMPOSE_MODE="podman-compose"
elif ! "${CONTAINER_CLI}" compose version >/dev/null 2>&1; then
    if [ "${CONTAINER_CLI}" = "podman" ] && command -v podman-compose >/dev/null 2>&1; then
        COMPOSE_CMD=("podman-compose")
        COMPOSE_MODE="podman-compose"
    else
        echo "[ERROR] '${CONTAINER_CLI} compose' is unavailable"
        echo "        Install compose support for ${CONTAINER_CLI} first"
        exit 1
    fi
fi

load_modules_token() {
    if [[ -n "${MODULES_TOKEN:-}" ]]; then
        return 0
    fi

    if [[ -n "${MODULES_TOKEN_FILE:-}" ]]; then
        if [[ ! -f "$MODULES_TOKEN_FILE" ]]; then
            echo "[ERROR] MODULES_TOKEN_FILE not found: ${MODULES_TOKEN_FILE}"
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

# 버전 관리 대상 디렉토리
VERSION_DIRS=(
    "hololive/hololive-kakao-bot-go"
)

# 인자 파싱
NO_BUMP=false
BUILD_ONLY=false
REMOTE_CACHE=false
TARGET_SERVICES=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --no-bump)
            NO_BUMP=true
            shift
            ;;
        --build-only)
            BUILD_ONLY=true
            shift
            ;;
        --remote-cache)
            REMOTE_CACHE=true
            shift
            ;;
        *)
            TARGET_SERVICES+=("$1")
            shift
            ;;
    esac
done

COMPOSE_FILES=(-f docker-compose.prod.yml)
if [ "$REMOTE_CACHE" = true ]; then
    if [ -z "${REMOTE_CACHE_PREFIX:-}" ]; then
        echo "[ERROR] --remote-cache requires REMOTE_CACHE_PREFIX"
        echo "        Example: REMOTE_CACHE_PREFIX=ghcr.io/<owner>"
        exit 1
    fi
    COMPOSE_FILES+=(-f docker-compose.remote-cache.yml)
fi

# 범프 대상 확인 함수
should_bump() {
    local dir_path=$1
    if [ ${#TARGET_SERVICES[@]} -eq 0 ]; then
        return 0
    fi
    for target in "${TARGET_SERVICES[@]}"; do
        if [[ "$dir_path" == *"$target"* ]]; then
            return 0
        fi
    done
    return 1
}

# Step 1: 버전 범프
if [ "$NO_BUMP" = false ]; then
    echo "[BUMP] Bumping patch versions..."
    for dir in "${VERSION_DIRS[@]}"; do
        if should_bump "$dir"; then
            if [ -f "$dir/Makefile" ] && [ -f "$dir/VERSION" ]; then
                old_version=$(cat "$dir/VERSION" | xargs)
                make -C "$dir" bump-patch --no-print-directory > /dev/null
                new_version=$(cat "$dir/VERSION" | xargs)
                echo "  [OK] $dir: $old_version -> $new_version"
            else
                echo "  [WARN] $dir: Makefile or VERSION not found, skipping"
            fi
        fi
    done
    echo ""
else
    echo "[SKIP] Skipping version bump (--no-bump set)"
    echo ""
fi

# Step 2: Docker Compose 빌드
echo "[BUILD] Building Docker images..."
echo "  CONTAINER_CLI=$CONTAINER_CLI"
echo "  COMPOSE_MODE=$COMPOSE_MODE"
echo "  REMOTE_CACHE=$REMOTE_CACHE"

# VERSION 파일에서 환경변수 설정 (docker-compose build args로 전달)
export HOLO_BOT_VERSION=$(cat hololive/hololive-kakao-bot-go/VERSION 2>/dev/null | xargs || echo "dev")

echo "  HOLO_BOT_VERSION=$HOLO_BOT_VERSION"
if [ "$REMOTE_CACHE" = true ]; then
    echo "  REMOTE_CACHE_PREFIX=$REMOTE_CACHE_PREFIX"
fi
echo ""

load_modules_token
require_modules_token

if [ ${#TARGET_SERVICES[@]} -gt 0 ]; then
    # 지정 타겟 빌드
    echo "  [Docker] Targets: ${TARGET_SERVICES[*]}"
    "${COMPOSE_CMD[@]}" "${COMPOSE_FILES[@]}" build "${TARGET_SERVICES[@]}"
elif [ "$BUILD_ONLY" = true ]; then
    # 전체 빌드만 수행
    echo "  Target: All Services (build only)"
    echo "  [Docker] ${COMPOSE_FILES[*]}"
    "${COMPOSE_CMD[@]}" "${COMPOSE_FILES[@]}" build
else
    # 전체 빌드 + 배포
    echo "  Target: All Services (build + deploy)"
    echo "  [Docker] ${COMPOSE_FILES[*]}"
    "${COMPOSE_CMD[@]}" "${COMPOSE_FILES[@]}" up -d --build
fi

echo ""
echo "[DONE] Build complete!"

# Step 4: 버전 리포트
echo ""
echo "[VERSIONS] Current versions:"
for dir in "${VERSION_DIRS[@]}"; do
    if [ -f "$dir/VERSION" ]; then
        printf "  %-40s : %s\n" "$dir" "$(cat "$dir/VERSION" | xargs)"
    fi
done
