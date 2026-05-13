#!/usr/bin/env bash
# build-all.sh: Hololive Bot 서비스 버전 관리 및 Docker 이미지 빌드 스크립트
#
# 사용법:
#   ./build-all.sh                      # 모든 서비스 버전 bump + 빌드/배포
#   ./build-all.sh --no-bump            # 버전 bump 없이 빌드/배포
#   ./build-all.sh --build-only         # 빌드만 수행 (배포/재기동 없음)
#   ./build-all.sh --remote-cache       # registry-backed remote cache 사용 (REMOTE_CACHE_PREFIX 필요)
#   ./build-all.sh --skip-local-ci      # 로컬 CI gate를 건너뜀
#   ./build-all.sh hololive-bot         # 특정 서비스만 빌드
#   ./build-all.sh bot                  # hololive-bot alias

set -Eeuo pipefail

# 스크립트 위치 기준 절대 경로로 이동한다. git common-dir를 쓰면 linked worktree에서
# 현재 작업트리가 아닌 원본 checkout의 shared-go를 참조할 수 있다.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "${SCRIPT_DIR}" rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

resolve_shared_go_workspace_path() {
    local candidate="${SHARED_GO_WORKSPACE_PATH:-${REPO_ROOT}/shared-go}"
    if [[ ! -d "${candidate}" ]]; then
        echo "[ERROR] Active shared-go workspace not found: ${candidate}" >&2
        exit 1
    fi

    (cd "${candidate}" && pwd)
}

resolve_iris_client_go_workspace_path() {
    local candidate="${IRIS_CLIENT_GO_WORKSPACE_PATH:-${REPO_ROOT}/../iris-client-go}"
    if [[ ! -d "${candidate}" ]]; then
        echo "[ERROR] Active iris-client-go workspace not found: ${candidate}" >&2
        exit 1
    fi

    (cd "${candidate}" && pwd)
}

export SHARED_GO_WORKSPACE_PATH="$(resolve_shared_go_workspace_path)"
export IRIS_CLIENT_GO_WORKSPACE_PATH="$(resolve_iris_client_go_workspace_path)"

# 컨테이너 런타임 CLI (docker / podman)
CONTAINER_CLI="${CONTAINER_CLI:-docker}"
case "${CONTAINER_CLI}" in
    docker|podman) ;;
    *)
        echo "[ERROR] Unsupported CONTAINER_CLI: ${CONTAINER_CLI}" >&2
        echo "        Allowed values: docker, podman" >&2
        exit 1
        ;;
esac
if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
    echo "[ERROR] Container CLI not found: ${CONTAINER_CLI}" >&2
    echo "        Set CONTAINER_CLI=docker or CONTAINER_CLI=podman" >&2
    exit 1
fi

COMPOSE_CMD=("${CONTAINER_CLI}" "compose")
COMPOSE_MODE="${CONTAINER_CLI} compose"
# podman 선택 시 docker-compose provider 의존을 피하기 위해 podman-compose 우선
if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=("podman-compose")
    COMPOSE_MODE="podman-compose"
elif ! "${CONTAINER_CLI}" compose version >/dev/null 2>&1; then
    if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
        COMPOSE_CMD=("podman-compose")
        COMPOSE_MODE="podman-compose"
    else
        echo "[ERROR] '${CONTAINER_CLI} compose' is unavailable" >&2
        echo "        Install compose support for ${CONTAINER_CLI} first" >&2
        exit 1
    fi
fi

# 버전 관리 대상 디렉토리
VERSION_DIRS=(
    "hololive/hololive-kakao-bot-go"
    "hololive/hololive-admin-api"
    "hololive/hololive-alarm-worker"
)

declare -A COMPOSE_SERVICE_BY_ALIAS=(
    [bot]="hololive-bot"
    [hololive-bot]="hololive-bot"
    [hololive-kakao-bot-go]="hololive-bot"
    [admin-api]="hololive-admin-api"
    [hololive-admin-api]="hololive-admin-api"
    [alarm-worker]="hololive-alarm-worker"
    [hololive-alarm-worker]="hololive-alarm-worker"
    [dispatcher]="dispatcher-go"
    [dispatcher-go]="dispatcher-go"
    [stream-ingester]="stream-ingester"
    [youtube-scraper]="youtube-scraper"
    [llm-scheduler]="llm-scheduler"
    [admin-dashboard]="admin-dashboard"
)

declare -A VERSION_DIR_BY_SERVICE=(
    [hololive-bot]="hololive/hololive-kakao-bot-go"
    [hololive-admin-api]="hololive/hololive-admin-api"
    [hololive-alarm-worker]="hololive/hololive-alarm-worker"
)

# 인자 파싱
NO_BUMP=false
BUILD_ONLY=false
REMOTE_CACHE=false
SKIP_LOCAL_CI=false
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
        --skip-local-ci)
            SKIP_LOCAL_CI=true
            shift
            ;;
        --help|-h)
            sed -n '1,10p' "$0"
            exit 0
            ;;
        --*)
            echo "[ERROR] Unknown option: $1" >&2
            exit 1
            ;;
        *)
            service="${COMPOSE_SERVICE_BY_ALIAS[$1]:-}"
            if [[ -z "${service}" ]]; then
                echo "[ERROR] Unknown build target: $1" >&2
                echo "        Known targets: ${!COMPOSE_SERVICE_BY_ALIAS[*]}" >&2
                exit 1
            fi
            TARGET_SERVICES+=("${service}")
            shift
            ;;
    esac
done

COMPOSE_FILES=(-f docker-compose.prod.yml)
if [[ "${REMOTE_CACHE}" == true ]]; then
    if [[ -z "${REMOTE_CACHE_PREFIX:-}" ]]; then
        echo "[ERROR] --remote-cache requires REMOTE_CACHE_PREFIX" >&2
        echo "        Example: REMOTE_CACHE_PREFIX=ghcr.io/<owner>" >&2
        exit 1
    fi
    COMPOSE_FILES+=(-f docker-compose.remote-cache.yml)
fi

read_version() {
    local dir_path="$1"
    local fallback="$2"
    local version_file="${dir_path}/VERSION"
    local value=""

    if [[ -f "${version_file}" ]]; then
        value="$(xargs < "${version_file}")"
    fi

    if [[ -n "${value}" ]]; then
        printf '%s\n' "${value}"
    else
        printf '%s\n' "${fallback}"
    fi
}

# 범프 대상 확인 함수
should_bump() {
    local dir_path="$1"
    local target_service
    if [[ ${#TARGET_SERVICES[@]} -eq 0 ]]; then
        return 0
    fi
    for target_service in "${TARGET_SERVICES[@]}"; do
        if [[ "${VERSION_DIR_BY_SERVICE[$target_service]:-}" == "${dir_path}" ]]; then
            return 0
        fi
    done
    return 1
}

read_compose_env_value() {
    local key="$1"
    local env_file="${COMPOSE_ENV_FILE:-./.env}"
    local value=""

    value="${!key:-}"
    if [[ -n "${value}" ]]; then
        printf '%s\n' "${value}"
        return 0
    fi

    if [[ -f "${env_file}" ]]; then
        value="$(awk -F= -v k="${key}" '
            $0 !~ /^[[:space:]]*#/ && $1 == k {
              v = substr($0, index($0, "=") + 1)
            }
            END { print v }
        ' "${env_file}")"
        value="${value%$'\r'}"
        case "${value}" in
            \"*\")
                value="${value#\"}"
                value="${value%\"}"
                ;;
            \'*\')
                value="${value#\'}"
                value="${value%\'}"
                ;;
        esac
    fi

    printf '%s\n' "${value}"
}

validate_runtime_config_for_deploy() {
    # build-only와 단일 target 빌드는 런타임 파일이 필요 없다. up -d 전에만 운영 파일 누락을 막는다.
    if [[ "${BUILD_ONLY}" == true || ${#TARGET_SERVICES[@]} -gt 0 ]]; then
        return 0
    fi

    local runtime_config_dir="${RUNTIME_CONFIG_DIR:-${REPO_ROOT}/runtime-config}"
    local host_iris_base_url_file="${runtime_config_dir}/iris_base_url"
    local container_iris_base_url_file="/app/runtime-config/iris_base_url"
    local iris_base_url
    local iris_base_url_file
    iris_base_url="$(read_compose_env_value IRIS_BASE_URL)"
    iris_base_url_file="$(read_compose_env_value IRIS_BASE_URL_FILE)"

    if [[ -n "${iris_base_url_file}" && "${iris_base_url_file}" == "${container_iris_base_url_file}" && ! -s "${host_iris_base_url_file}" ]]; then
        echo "[ERROR] IRIS_BASE_URL_FILE points to ${container_iris_base_url_file}, but ${host_iris_base_url_file} is missing or empty" >&2
        exit 1
    fi

    if [[ -z "${iris_base_url}" && -z "${iris_base_url_file}" ]]; then
        if [[ -s "${host_iris_base_url_file}" ]]; then
            export IRIS_BASE_URL_FILE="${container_iris_base_url_file}"
            echo "[INFO] Using file-based IRIS_BASE_URL: ${IRIS_BASE_URL_FILE}"
        else
            echo "[ERROR] IRIS_BASE_URL and IRIS_BASE_URL_FILE are both empty, and ${host_iris_base_url_file} does not exist or is empty" >&2
            echo "        Either set IRIS_BASE_URL in ${COMPOSE_ENV_FILE:-./.env}, set IRIS_BASE_URL_FILE, or create runtime-config/iris_base_url" >&2
            exit 1
        fi
    fi
}

stop_legacy_dispatcher_after_default_deploy() {
    if [[ ",${COMPOSE_PROFILES:-}," == *",legacy-dispatcher-go,"* ]]; then
        return 0
    fi

    local container_id
    container_id="$("${CONTAINER_CLI}" ps -aq --filter "name=^hololive-dispatcher-go$" 2>/dev/null || true)"
    if [[ -z "${container_id}" ]]; then
        return 0
    fi

    echo "[CLEANUP] Stopping legacy dispatcher-go outside the default production profile"
    "${CONTAINER_CLI}" stop hololive-dispatcher-go >/dev/null 2>&1 || true
    "${CONTAINER_CLI}" rm -f hololive-dispatcher-go >/dev/null
}

# Step 0: 로컬 CI gate
if [[ "${SKIP_LOCAL_CI}" == false ]]; then
    echo "[CHECK] Running local CI gate before build..."
    ./scripts/ci/local-ci.sh
    echo ""
else
    echo "[SKIP] Skipping local CI gate (--skip-local-ci set)"
    echo ""
fi

# Step 1: 버전 범프
if [[ "${NO_BUMP}" == false ]]; then
    echo "[BUMP] Bumping patch versions..."
    for dir in "${VERSION_DIRS[@]}"; do
        if should_bump "${dir}"; then
            if [[ -f "${dir}/Makefile" && -f "${dir}/VERSION" ]]; then
                old_version="$(read_version "${dir}" dev)"
                make -C "${dir}" bump-patch --no-print-directory >/dev/null
                new_version="$(read_version "${dir}" dev)"
                echo "  [OK] ${dir}: ${old_version} -> ${new_version}"
            else
                echo "  [WARN] ${dir}: Makefile or VERSION not found, skipping" >&2
            fi
        fi
    done
    echo ""
else
    echo "[SKIP] Skipping version bump (--no-bump set)"
    echo ""
fi

# Step 2: Docker Compose 빌드
validate_runtime_config_for_deploy

echo "[BUILD] Building Docker images..."
echo "  CONTAINER_CLI=${CONTAINER_CLI}"
echo "  COMPOSE_MODE=${COMPOSE_MODE}"
echo "  REMOTE_CACHE=${REMOTE_CACHE}"
echo "  SHARED_GO_WORKSPACE_PATH=${SHARED_GO_WORKSPACE_PATH}"
echo "  IRIS_CLIENT_GO_WORKSPACE_PATH=${IRIS_CLIENT_GO_WORKSPACE_PATH}"

# VERSION 파일에서 환경변수 설정 (docker-compose build args로 전달)
export HOLO_BOT_VERSION="$(read_version hololive/hololive-kakao-bot-go dev)"
export HOLO_ADMIN_API_VERSION="$(read_version hololive/hololive-admin-api "${HOLO_BOT_VERSION}")"
export HOLO_ALARM_WORKER_VERSION="$(read_version hololive/hololive-alarm-worker "${HOLO_BOT_VERSION}")"

echo "  HOLO_BOT_VERSION=${HOLO_BOT_VERSION}"
echo "  HOLO_ADMIN_API_VERSION=${HOLO_ADMIN_API_VERSION}"
echo "  HOLO_ALARM_WORKER_VERSION=${HOLO_ALARM_WORKER_VERSION}"
if [[ "${REMOTE_CACHE}" == true ]]; then
    echo "  REMOTE_CACHE_PREFIX=${REMOTE_CACHE_PREFIX}"
fi
echo ""

if [[ ${#TARGET_SERVICES[@]} -gt 0 ]]; then
    # 지정 타겟 빌드
    echo "  [Docker] Targets: ${TARGET_SERVICES[*]}"
    "${COMPOSE_CMD[@]}" "${COMPOSE_FILES[@]}" build "${TARGET_SERVICES[@]}"
elif [[ "${BUILD_ONLY}" == true ]]; then
    # 전체 빌드만 수행
    echo "  Target: All Services (build only)"
    printf '  [Docker]'; printf ' %q' "${COMPOSE_FILES[@]}"; echo ""
    "${COMPOSE_CMD[@]}" "${COMPOSE_FILES[@]}" build
else
    # 전체 빌드 + 배포
    echo "  Target: All Services (build + deploy)"
    printf '  [Docker]'; printf ' %q' "${COMPOSE_FILES[@]}"; echo ""
    "${COMPOSE_CMD[@]}" "${COMPOSE_FILES[@]}" up -d --build
    stop_legacy_dispatcher_after_default_deploy
fi

echo ""
echo "[DONE] Build complete!"

# Step 3: 버전 리포트
echo ""
echo "[VERSIONS] Current versions:"
for dir in "${VERSION_DIRS[@]}"; do
    if [[ -f "${dir}/VERSION" ]]; then
        printf "  %-40s : %s\n" "${dir}" "$(read_version "${dir}" dev)"
    fi
done
