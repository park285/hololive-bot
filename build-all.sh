#!/bin/bash
# build-all.sh: Hololive Bot 서비스 버전 관리 및 Docker 이미지 빌드 스크립트
#
# 사용법:
#   ./build-all.sh                      # 모든 서비스 버전 bump + 빌드
#   ./build-all.sh --no-bump            # 버전 bump 없이 빌드만
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

# 버전 관리 대상 디렉토리
VERSION_DIRS=(
    "hololive/hololive-kakao-bot-go"
)

# Rust 서비스 목록 (Podman 직접 빌드)
HOLO_RS_SERVICES=("hololive-scraper" "hololive-alarm")

# 인자 파싱
NO_BUMP=false
TARGET_SERVICES=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --no-bump)
            NO_BUMP=true
            shift
            ;;
        *)
            TARGET_SERVICES+=("$1")
            shift
            ;;
    esac
done

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

# VERSION 파일에서 환경변수 설정 (docker-compose build args로 전달)
export HOLO_BOT_VERSION=$(cat hololive/hololive-kakao-bot-go/VERSION 2>/dev/null | xargs || echo "dev")

echo "  HOLO_BOT_VERSION=$HOLO_BOT_VERSION"
echo ""

if [ ${#TARGET_SERVICES[@]} -gt 0 ]; then
    # 지정 타겟 빌드
    echo "  [Docker] Targets: ${TARGET_SERVICES[*]}"
    "${COMPOSE_CMD[@]}" -f docker-compose.prod.yml build "${TARGET_SERVICES[@]}"
else
    # 전체 빌드
    echo "  Target: All Services"
    echo "  [Docker] docker-compose.prod.yml"
    "${COMPOSE_CMD[@]}" -f docker-compose.prod.yml up -d --build
fi

# Step 3: Rust 서비스 Podman 빌드 (CONTAINER_CLI=podman 일 때만)
if [ "${CONTAINER_CLI}" = "podman" ]; then
    echo ""
    echo "[BUILD] Building Rust services with Podman..."
    for svc in "${HOLO_RS_SERVICES[@]}"; do
        if [ ${#TARGET_SERVICES[@]} -eq 0 ] || [[ " ${TARGET_SERVICES[*]} " == *" $svc "* ]]; then
            echo "  [Podman] Building $svc..."
            podman build -t "$svc:${HOLO_BOT_VERSION}" -f "hololive/${svc}-rs/Dockerfile" "hololive/${svc}-rs" || \
                echo "  [WARN] $svc build failed or Dockerfile not found, skipping"
        fi
    done
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
