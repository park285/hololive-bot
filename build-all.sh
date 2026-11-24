#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "${SCRIPT_DIR}" rev-parse --show-toplevel)"
cd "${REPO_ROOT}"
. "${REPO_ROOT}/scripts/deploy/lib/source-revision.sh"

read_version() {
    local file="$1/VERSION"
    local -a lines=()

    if [[ ! -f "${file}" ]]; then
        echo "[ERROR] Missing ${file}" >&2
        return 1
    fi
    mapfile -t lines <"${file}"
    if [[ "${#lines[@]}" -ne 1 || ! "${lines[0]}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
        echo "[ERROR] ${file} must hold one MAJOR.MINOR.PATCH line" >&2
        return 1
    fi
    printf '%s\n' "${lines[0]}"
}

declare -A IMAGE_BY_SERVICE=(
    [hololive-api]=localhost/hololive-api:prod
    [hololive-alarm-worker]=localhost/hololive-alarm-worker:prod
    [collector]=localhost/hololive-collector:prod
    [youtube-po-c]=localhost/hololive-youtube-po-sandbox:prod
    [holo-postgres]=localhost/hololive-postgres:prod
    [valkey-cache]=localhost/iris-stack-valkey:9.1.3-zlib1.4-r1
    [admin-dashboard-ingress]=localhost/iris-stack-nginx:1.31.9-zlib1.4-r0
)
declare -A CONTEXT_BY_SERVICE=(
    [holo-postgres]=deploy/images/postgres
    [valkey-cache]=deploy/images/valkey
    [admin-dashboard-ingress]=deploy/images/nginx
)
declare -A DOCKERFILE_BY_SERVICE=(
    [hololive-api]=hololive/hololive-api/Dockerfile
    [hololive-alarm-worker]=hololive/hololive-alarm-worker/Dockerfile
    [collector]=hololive/hololive-collector/Dockerfile
    [youtube-po-c]=hololive/hololive-collector/Dockerfile.po-sandbox
)
VERSION_DIRS=(
    "hololive/hololive-api"
    "hololive/hololive-alarm-worker"
)
declare -A VERSION_DIR_BY_SERVICE=(
    [hololive-api]="hololive/hololive-api"
    [hololive-alarm-worker]="hololive/hololive-alarm-worker"
)

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
    printf '  %s\n' "${!IMAGE_BY_SERVICE[@]}" | LC_ALL=C sort
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
            if [[ -z "${IMAGE_BY_SERVICE[$1]:-}" ]]; then
                echo "[ERROR] Unknown build target: $1" >&2
                usage >&2
                exit 1
            fi
            TARGET_SERVICES+=("$1")
            ;;
    esac
    shift
done

if [[ "${BUILD_ONLY}" == false && ${#TARGET_SERVICES[@]} -eq 0 ]]; then
    echo "[ERROR] build-all.sh only builds images; pass --build-only or a target." >&2
    echo "        Central deployment follows docs/current/runbooks/release.md (image transfer + Quadlet units)." >&2
    exit 2
fi

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
        old_version="$(read_version "${dir}")"
        make -C "${dir}" bump-patch --no-print-directory >/dev/null
        new_version="$(read_version "${dir}")"
        echo "  ${dir}: ${old_version} -> ${new_version}"
    done
else
    echo "[SKIP] Version bump disabled by --no-bump"
fi

HOLO_API_VERSION="$(read_version hololive/hololive-api)"
HOLO_ALARM_WORKER_VERSION="$(read_version hololive/hololive-alarm-worker)"
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
    if [[ " ${BUILD_SERVICES[*]} " == *" collector "* && " ${BUILD_SERVICES[*]} " != *" youtube-po-c "* ]]; then
        BUILD_SERVICES+=(youtube-po-c)
    fi
else
    BUILD_SERVICES=(hololive-api hololive-alarm-worker collector youtube-po-c holo-postgres valkey-cache admin-dashboard-ingress)
fi

build_service() {
    local service="$1"
    local image="${IMAGE_BY_SERVICE[$service]}"
    local -a args=(build "${platform_args[@]}" --format docker --tag "${image}"
        --build-arg "REVISION=${REVISION}" --label "org.opencontainers.image.revision=${REVISION}")

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
    version="$(read_version "${dir}")"
    printf "  %-40s : %s\n" "${dir}" "${version}"
done
