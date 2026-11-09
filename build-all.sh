#!/usr/bin/env bash
# 중앙 Quadlet이 쓰는 production image를 Podman으로 빌드하고 revision label을 검증한다.
# 배포는 하지 않는다. 중앙 배포는 docs/current/runbooks/release.md의 전송·Quadlet 기동 절차가 소유한다.

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "${SCRIPT_DIR}" rev-parse --show-toplevel)"
cd "${REPO_ROOT}"
. "${REPO_ROOT}/scripts/deploy/lib/compose-services.sh"
. "${REPO_ROOT}/scripts/deploy/lib/source-revision.sh"

read_version() {
    local dir_path="$1"
    local fallback="$2"
    local value=""

    if [[ -f "${dir_path}/VERSION" ]]; then
        value="$(xargs <"${dir_path}/VERSION")"
    fi
    if [[ -n "${value}" ]]; then
        printf '%s\n' "${value}"
    else
        printf '%s\n' "${fallback}"
    fi
}

usage() {
    cat <<'EOF'
Usage: ./build-all.sh [--no-bump] [--skip-local-ci] (--build-only | <target>...)

Options:
  --no-bump        Keep runtime VERSION files unchanged.
  --build-only     Build every production image.
  --skip-local-ci  Skip scripts/ci/local-ci.sh before building.

Environment:
  HOLOLIVE_BUILD_PLATFORM  Target platform such as linux/arm64. Empty builds for this host.
EOF
    echo
    echo "Build targets:"
    compose_service_build_targets_text | sed 's/^/  /'
}

NO_BUMP=false
BUILD_ONLY=false
SKIP_LOCAL_CI=false
TARGET_SERVICES=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --no-bump) NO_BUMP=true ;;
        --build-only) BUILD_ONLY=true ;;
        --skip-local-ci) SKIP_LOCAL_CI=true ;;
        --help|-h)
            usage
            exit 0
            ;;
        --*)
            echo "[ERROR] Unknown option: $1" >&2
            exit 1
            ;;
        *)
            if ! service="$(compose_service_resolve_build_target "$1")"; then
                echo "[ERROR] Unknown build target: $1" >&2
                usage >&2
                exit 1
            fi
            TARGET_SERVICES+=("${service}")
            ;;
    esac
    shift
done

if [[ "${BUILD_ONLY}" == false && ${#TARGET_SERVICES[@]} -eq 0 ]]; then
    echo "[ERROR] build-all.sh only builds images; pass --build-only or a target." >&2
    echo "        Central deployment follows docs/current/runbooks/release.md (image transfer + Quadlet units)." >&2
    exit 2
fi

# 이미지 이름은 Quadlet unit의 기본 Image=와 같다. 중앙 unit은 배포 때 sha256 identity로 고정한다.
declare -A IMAGE_BY_SERVICE=(
    [hololive-api]=localhost/hololive-api:prod
    [hololive-alarm-worker]=localhost/hololive-alarm-worker:prod
    [youtube-collector]=localhost/hololive-youtube-collector:prod
    [youtube-po-c]=localhost/hololive-youtube-po-sandbox:prod
    [holo-postgres]=localhost/hololive-postgres:prod
    [valkey-cache]=localhost/iris-stack-valkey:9.1.3-zlib1.4-r0
    [admin-dashboard-ingress]=localhost/iris-stack-nginx:1.31.8-zlib1.4-r0
)
declare -A CONTEXT_BY_SERVICE=(
    [holo-postgres]=deploy/images/postgres
    [valkey-cache]=deploy/images/valkey
    [admin-dashboard-ingress]=deploy/images/nginx
)
declare -A DOCKERFILE_BY_SERVICE=(
    [hololive-api]=hololive/hololive-api/Dockerfile
    [hololive-alarm-worker]=hololive/hololive-alarm-worker/Dockerfile
    [youtube-collector]=hololive/hololive-youtube-collector/Dockerfile
    [youtube-po-c]=hololive/hololive-youtube-collector/Dockerfile.po-sandbox
)
VERSION_DIRS=(
    "hololive/hololive-api"
    "hololive/hololive-alarm-worker"
)
declare -A VERSION_DIR_BY_SERVICE=(
    [hololive-api]="hololive/hololive-api"
    [hololive-alarm-worker]="hololive/hololive-alarm-worker"
)

# arm64 image의 RUN은 x86_64 build host에서 binfmt 등록이 있어야 실행된다. 자동 설치는 host 변경이므로
# 하지 않고 등록이 없으면 빌드 전에 멈춘다.
BUILD_PLATFORM="${HOLOLIVE_BUILD_PLATFORM:-}"
platform_args=()
if [[ -n "${BUILD_PLATFORM}" ]]; then
    [[ "${BUILD_PLATFORM}" =~ ^linux/(amd64|arm64)$ ]] || {
        echo "[ERROR] HOLOLIVE_BUILD_PLATFORM must be linux/amd64 or linux/arm64" >&2
        exit 2
    }
    platform_args=(--platform "${BUILD_PLATFORM}")
    host_arch="$(podman info --format '{{.Host.Arch}}')"
    if [[ "${BUILD_PLATFORM#linux/}" != "${host_arch}" && "${BUILD_PLATFORM}" == linux/arm64 ]] \
        && ! grep -qx enabled /proc/sys/fs/binfmt_misc/qemu-aarch64 2>/dev/null; then
        echo "[ERROR] linux/arm64 build on ${host_arch} needs a registered qemu-aarch64 binfmt handler" >&2
        exit 1
    fi
fi

should_bump() {
    local dir_path="$1"
    local target_service=""

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

if [[ "${SKIP_LOCAL_CI}" == false ]]; then
    echo "[CHECK] Running local CI gate before build"
    ./scripts/ci/local-ci.sh
else
    echo "[SKIP] Local CI gate disabled by --skip-local-ci"
fi

if [[ "${NO_BUMP}" == false ]]; then
    echo "[BUMP] Bumping patch versions"
    for dir in "${VERSION_DIRS[@]}"; do
        if ! should_bump "${dir}"; then
            continue
        fi
        if [[ ! -f "${dir}/Makefile" || ! -f "${dir}/VERSION" ]]; then
            echo "[ERROR] Version contract missing in ${dir}" >&2
            exit 1
        fi
        old_version="$(read_version "${dir}" dev)"
        make -C "${dir}" bump-patch --no-print-directory >/dev/null
        new_version="$(read_version "${dir}" dev)"
        echo "  ${dir}: ${old_version} -> ${new_version}"
    done
else
    echo "[SKIP] Version bump disabled by --no-bump"
fi

HOLO_API_VERSION="$(read_version hololive/hololive-api dev)"
HOLO_ALARM_WORKER_VERSION="$(read_version hololive/hololive-alarm-worker "${HOLO_API_VERSION}")"
REVISION=unknown
if ! build_worktree_state="$(git -C "${REPO_ROOT}" status --porcelain=v1 --untracked-files=all)"; then
    echo "[ERROR] Failed to inspect build source worktree" >&2
    exit 1
fi
if [[ -z "${build_worktree_state}" ]]; then
    REVISION="$(deploy_source_revision "${REPO_ROOT}")"
fi

if [[ ${#TARGET_SERVICES[@]} -gt 0 ]]; then
    BUILD_SERVICES=("${TARGET_SERVICES[@]}")
    # collector와 PO issuer는 같은 source revision으로 짝지어 배포한다.
    if [[ " ${BUILD_SERVICES[*]} " == *" youtube-collector "* && " ${BUILD_SERVICES[*]} " != *" youtube-po-c "* ]]; then
        BUILD_SERVICES+=(youtube-po-c)
    fi
else
    BUILD_SERVICES=(hololive-api hololive-alarm-worker youtube-collector youtube-po-c holo-postgres valkey-cache admin-dashboard-ingress)
fi

build_service() {
    local service="$1"
    local image="${IMAGE_BY_SERVICE[$service]}"
    # HEALTHCHECK 등 docker 형식 메타데이터를 보존하고 docker-archive 저장 전후 config digest(identity)가 같도록
    # docker 형식으로 만든다.
    local -a args=(build "${platform_args[@]}" --format docker --tag "${image}" --build-arg "REVISION=${REVISION}")

    if [[ -n "${DOCKERFILE_BY_SERVICE[$service]:-}" ]]; then
        local version="${HOLO_API_VERSION}"
        [[ "${service}" != hololive-alarm-worker ]] || version="${HOLO_ALARM_WORKER_VERSION}"
        args+=(--build-arg "VERSION=${version}" --file "${DOCKERFILE_BY_SERVICE[$service]}" .)
    else
        args+=("${CONTEXT_BY_SERVICE[$service]}")
    fi
    echo "[BUILD] ${service} -> ${image}"
    podman "${args[@]}"
    if [[ "${REVISION}" != unknown ]]; then
        deploy_verify_object_revision podman image "${image}" "${REVISION}"
    fi
}

echo "[INFO] BUILD_PLATFORM=${BUILD_PLATFORM:-host}"
echo "[INFO] HOLO_API_VERSION=${HOLO_API_VERSION}"
echo "[INFO] HOLO_ALARM_WORKER_VERSION=${HOLO_ALARM_WORKER_VERSION}"
echo "[INFO] REVISION=${REVISION}"
for service in "${BUILD_SERVICES[@]}"; do
    build_service "${service}"
done
echo "[DONE] Image build complete"

echo
echo "[VERSIONS]"
for dir in "${VERSION_DIRS[@]}"; do
    printf "  %-40s : %s\n" "${dir}" "$(read_version "${dir}" dev)"
done
