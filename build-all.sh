#!/usr/bin/env bash
# Build, validate and optionally deploy the production three-runtime topology.
#
# Usage:
#   ./build-all.sh
#   ./build-all.sh --no-bump
#   ./build-all.sh --build-only
#   ./build-all.sh --remote-cache
#   ./build-all.sh --skip-local-ci
#   ./build-all.sh hololive-api
#   ./build-all.sh alarm-worker

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "${SCRIPT_DIR}" rev-parse --show-toplevel)"
cd "${REPO_ROOT}"
. "${REPO_ROOT}/scripts/deploy/lib/compose-env.sh"
. "${REPO_ROOT}/scripts/deploy/lib/compose-services.sh"
. "${REPO_ROOT}/scripts/deploy/lib/removed-runtimes.sh"

resolve_workspace_path() {
    local explicit_value="$1"
    local sibling_path="$2"
    local embedded_path="$3"
    local label="$4"
    local candidate="${explicit_value}"

    if [[ -z "${candidate}" ]]; then
        if [[ -d "${sibling_path}" ]]; then
            candidate="${sibling_path}"
        elif [[ -d "${embedded_path}" ]]; then
            candidate="${embedded_path}"
        fi
    fi
    if [[ ! -d "${candidate}" ]]; then
        echo "[ERROR] Active ${label} workspace not found" >&2
        exit 1
    fi
    (cd "${candidate}" && pwd)
}

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
    sed -n '1,12p' "$0"
    echo
    echo "Build targets:"
    compose_service_build_targets_text | sed 's/^/  /'
}

SHARED_GO_WORKSPACE_PATH="$(resolve_workspace_path \
    "${SHARED_GO_WORKSPACE_PATH:-}" \
    "${REPO_ROOT}/../shared-go" \
    "${REPO_ROOT}/shared-go" \
    "shared-go")"
IRIS_CLIENT_GO_WORKSPACE_PATH="$(resolve_workspace_path \
    "${IRIS_CLIENT_GO_WORKSPACE_PATH:-}" \
    "${REPO_ROOT}/../iris-client-go" \
    "${REPO_ROOT}/iris-client-go" \
    "iris-client-go")"
export SHARED_GO_WORKSPACE_PATH IRIS_CLIENT_GO_WORKSPACE_PATH

CONTAINER_CLI="${CONTAINER_CLI:-docker}"
case "${CONTAINER_CLI}" in
    docker|podman) ;;
    *)
        echo "[ERROR] Unsupported CONTAINER_CLI: ${CONTAINER_CLI}" >&2
        exit 1
        ;;
esac
if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
    echo "[ERROR] Container CLI not found: ${CONTAINER_CLI}" >&2
    exit 1
fi

COMPOSE_CMD=("${CONTAINER_CLI}" compose)
COMPOSE_MODE="${CONTAINER_CLI} compose"
if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=(podman-compose)
    COMPOSE_MODE=podman-compose
elif ! "${CONTAINER_CLI}" compose version >/dev/null 2>&1; then
    echo "[ERROR] '${CONTAINER_CLI} compose' is unavailable" >&2
    exit 1
fi

VERSION_DIRS=(
    "hololive/hololive-api"
    "hololive/hololive-alarm-worker"
)
declare -A VERSION_DIR_BY_SERVICE=(
    [hololive-api]="hololive/hololive-api"
    [hololive-alarm-worker]="hololive/hololive-alarm-worker"
)

NO_BUMP=false
BUILD_ONLY=false
REMOTE_CACHE=false
SKIP_LOCAL_CI=false
TARGET_SERVICES=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --no-bump)
            NO_BUMP=true
            ;;
        --build-only)
            BUILD_ONLY=true
            ;;
        --remote-cache)
            REMOTE_CACHE=true
            ;;
        --skip-local-ci)
            SKIP_LOCAL_CI=true
            ;;
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

COMPOSE_FILE_PATHS=(deploy/compose/docker-compose.prod.yml)
COMPOSE_FILES=(-f deploy/compose/docker-compose.prod.yml)
if [[ "${REMOTE_CACHE}" == true ]]; then
    if [[ -z "${REMOTE_CACHE_PREFIX:-}" ]]; then
        echo "[ERROR] --remote-cache requires REMOTE_CACHE_PREFIX" >&2
        exit 1
    fi
    COMPOSE_FILE_PATHS+=(deploy/compose/docker-compose.remote-cache.yml)
    COMPOSE_FILES+=(-f deploy/compose/docker-compose.remote-cache.yml)
fi

if ! COMPOSE_ENV_FILE="$(compose_env_resolve_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE
compose_env_validate_file_format "${COMPOSE_ENV_FILE}"
compose_env_assert_shell_matches_all_file_keys "${COMPOSE_ENV_FILE}"
compose_env_assert_no_shell_shadow_for_compose_files "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_PATHS[@]}"
compose_env_assert_admin_dashboard_loopback_bind "${COMPOSE_ENV_FILE}"

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

read_compose_env_value() {
    compose_env_read_value_from_file "${COMPOSE_ENV_FILE}" "$1"
}

validate_runtime_config_for_deploy() {
    if [[ "${BUILD_ONLY}" == true || ${#TARGET_SERVICES[@]} -gt 0 ]]; then
        return 0
    fi

    local runtime_config_dir="${RUNTIME_CONFIG_DIR:-${REPO_ROOT}/runtime-config}"
    local host_iris_base_url_file="${runtime_config_dir}/iris_base_url"
    local container_iris_base_url_file="/app/runtime-config/iris_base_url"
    local iris_base_url=""
    local iris_base_url_file=""

    iris_base_url="$(read_compose_env_value IRIS_BASE_URL)"
    iris_base_url_file="$(read_compose_env_value IRIS_BASE_URL_FILE)"

    if [[ -n "${iris_base_url_file}" \
       && "${iris_base_url_file}" == "${container_iris_base_url_file}" \
       && ! -s "${host_iris_base_url_file}" ]]; then
        echo "[ERROR] ${host_iris_base_url_file} is missing or empty" >&2
        exit 1
    fi

    if [[ -z "${iris_base_url}" && -z "${iris_base_url_file}" ]]; then
        if [[ -s "${host_iris_base_url_file}" ]]; then
            export IRIS_BASE_URL_FILE="${container_iris_base_url_file}"
            echo "[INFO] Using file-based IRIS_BASE_URL: ${IRIS_BASE_URL_FILE}"
        else
            echo "[ERROR] IRIS_BASE_URL is not configured" >&2
            exit 1
        fi
    fi
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
export HOLO_API_VERSION HOLO_ALARM_WORKER_VERSION

validate_runtime_config_for_deploy

echo "[INFO] CONTAINER_CLI=${CONTAINER_CLI}"
echo "[INFO] COMPOSE_MODE=${COMPOSE_MODE}"
echo "[INFO] REMOTE_CACHE=${REMOTE_CACHE}"
echo "[INFO] HOLO_API_VERSION=${HOLO_API_VERSION}"
echo "[INFO] HOLO_ALARM_WORKER_VERSION=${HOLO_ALARM_WORKER_VERSION}"
echo "[INFO] SHARED_GO_WORKSPACE_PATH=${SHARED_GO_WORKSPACE_PATH}"
echo "[INFO] IRIS_CLIENT_GO_WORKSPACE_PATH=${IRIS_CLIENT_GO_WORKSPACE_PATH}"
echo "[INFO] COMPOSE_ENV_FILE=${COMPOSE_ENV_FILE}"

"${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILES[@]}" config --quiet

if [[ ${#TARGET_SERVICES[@]} -gt 0 ]]; then
    echo "[BUILD] Targets: ${TARGET_SERVICES[*]}"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILES[@]}" build "${TARGET_SERVICES[@]}"
    echo "[DONE] Target image build complete"
elif [[ "${BUILD_ONLY}" == true ]]; then
    echo "[BUILD] All active buildable services"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILES[@]}" build
    echo "[DONE] Image build complete"
else
    compose_env_assert_live_compat_for_host_networked_postgres "${COMPOSE_FILE_PATHS[@]}"

    echo "[BUILD] All active buildable services"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILES[@]}" build

    echo "[CUTOVER] Replacing retired runtimes with the three-runtime topology"
    removed_runtime_cleanup_before_cutover
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILES[@]}" up -d --no-build

    echo "[VERIFY] Compose service state"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILES[@]}" ps
    echo "[DONE] Build and deployment complete"
fi

echo
echo "[VERSIONS]"
for dir in "${VERSION_DIRS[@]}"; do
    printf "  %-40s : %s\n" "${dir}" "$(read_version "${dir}" dev)"
done
