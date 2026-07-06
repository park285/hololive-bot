#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
. "${ROOT_DIR}/scripts/deploy/lib/compose-services.sh"
. "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh"
. "${ROOT_DIR}/scripts/deploy/lib/health-gate.sh"

compose_file_resolve_path() {
    local file="$1"
    if [[ ! -r "${file}" && -r "${ROOT_DIR}/deploy/compose/${file}" ]]; then
        printf '%s\n' "deploy/compose/${file}"
        return
    fi
    printf '%s\n' "${file}"
}

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

contains_compose_file() {
    local expected="$1"
    local path=""
    for path in "${COMPOSE_FILE_PATHS[@]}"; do
        if [[ "${path##*/}" == "${expected}" ]]; then
            return 0
        fi
    done
    return 1
}

usage() {
    echo "Usage: $0 <service|all>"
    echo
    echo "Supported services:"
    compose_service_redeploy_usage_lines
}

target_requires_db_migration() {
    case "${TARGET}" in
        hololive-api|hololive-alarm-worker|youtube-producer-c|"")
            return 0
            ;;
        youtube-producer)
            if contains_compose_file docker-compose.osaka.yml \
               || contains_compose_file docker-compose.osaka2.yml \
               || contains_compose_file docker-compose.seoul.yml; then
                return 1
            fi
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

run_db_migration_before_cutover() {
    echo "[BUILD] hololive-db-migrate"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" build hololive-db-migrate
    echo "[MIGRATE] hololive-db-migrate"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" run --rm hololive-db-migrate
}

if [[ $# -ne 1 ]]; then
    usage
    exit 1
fi

COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/docker-compose.prod.yml}"
IFS=':' read -r -a COMPOSE_FILE_PATHS <<<"${COMPOSE_FILE}"
COMPOSE_FILE_ARGS=()
for index in "${!COMPOSE_FILE_PATHS[@]}"; do
    file="${COMPOSE_FILE_PATHS[$index]}"
    [[ -n "${file}" ]] || continue
    file="$(compose_file_resolve_path "${file}")"
    COMPOSE_FILE_PATHS[$index]="${file}"
    COMPOSE_FILE_ARGS+=("-f" "${file}")
done
COMPOSE_FILE="$(IFS=:; printf '%s' "${COMPOSE_FILE_PATHS[*]}")"

SHARED_GO_WORKSPACE_PATH="$(resolve_workspace_path \
    "${SHARED_GO_WORKSPACE_PATH:-}" \
    "${ROOT_DIR}/../shared-go" \
    "${ROOT_DIR}/shared-go" \
    "shared-go")"
IRIS_CLIENT_GO_WORKSPACE_PATH="$(resolve_workspace_path \
    "${IRIS_CLIENT_GO_WORKSPACE_PATH:-}" \
    "${ROOT_DIR}/../iris-client-go" \
    "${ROOT_DIR}/iris-client-go" \
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

SERVICE="$1"
if ! TARGET="$(compose_service_resolve_redeploy_target "${SERVICE}")"; then
    echo "[ERROR] Unsupported service: ${SERVICE}" >&2
    echo >&2
    usage >&2
    exit 1
fi

if [[ "${TARGET}" == "youtube-producer" ]] \
   && ! contains_compose_file docker-compose.osaka.yml \
   && ! contains_compose_file docker-compose.osaka2.yml \
   && ! contains_compose_file docker-compose.seoul.yml \
   && [[ "${ALLOW_CENTRAL_YOUTUBE_PRODUCER:-}" != "true" ]]; then
    echo "[ERROR] youtube-producer is remote-AP-owned; central redeploy requires ALLOW_CENTRAL_YOUTUBE_PRODUCER=true" >&2
    exit 1
fi

if [[ "${TARGET}" == "youtube-producer-c" ]]; then
    if ! contains_compose_file docker-compose.main-ap.yml; then
        echo "[ERROR] youtube-producer-c requires docker-compose.main-ap.yml in COMPOSE_FILE" >&2
        exit 1
    fi
    if [[ ",${COMPOSE_PROFILES:-}," != *",main-ap,"* ]]; then
        echo "[ERROR] youtube-producer-c requires COMPOSE_PROFILES=main-ap" >&2
        exit 1
    fi
fi

if [[ -z "${TARGET}" ]] \
   && ! contains_compose_file docker-compose.osaka.yml \
   && ! contains_compose_file docker-compose.osaka2.yml \
   && ! contains_compose_file docker-compose.seoul.yml \
   && [[ ",${COMPOSE_PROFILES:-}," == *",oracle,"* ]] \
   && [[ "${ALLOW_CENTRAL_YOUTUBE_PRODUCER:-}" != "true" ]]; then
    echo "[ERROR] Central all-service deploy cannot enable the remote-owned oracle profile" >&2
    exit 1
fi

if ! COMPOSE_ENV_FILE="$(compose_env_resolve_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE
compose_env_validate_file_format "${COMPOSE_ENV_FILE}"
compose_env_assert_shell_matches_all_file_keys "${COMPOSE_ENV_FILE}"
compose_env_assert_no_shell_shadow_for_compose_files "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_PATHS[@]}"
compose_env_assert_admin_dashboard_loopback_bind "${COMPOSE_ENV_FILE}"
compose_env_assert_live_compat_for_host_networked_postgres "${COMPOSE_FILE_PATHS[@]}"

HOLO_API_VERSION="$(xargs <hololive/hololive-api/VERSION 2>/dev/null || printf '%s' dev)"
HOLO_ALARM_WORKER_VERSION="$(xargs <hololive/hololive-alarm-worker/VERSION 2>/dev/null || printf '%s' "${HOLO_API_VERSION}")"
export HOLO_API_VERSION HOLO_ALARM_WORKER_VERSION

echo "[INFO] COMPOSE_MODE=${COMPOSE_MODE}"
echo "[INFO] COMPOSE_FILE=${COMPOSE_FILE}"
echo "[INFO] HOLO_API_VERSION=${HOLO_API_VERSION}"
echo "[INFO] HOLO_ALARM_WORKER_VERSION=${HOLO_ALARM_WORKER_VERSION}"
echo "[INFO] SHARED_GO_WORKSPACE_PATH=${SHARED_GO_WORKSPACE_PATH}"
echo "[INFO] IRIS_CLIENT_GO_WORKSPACE_PATH=${IRIS_CLIENT_GO_WORKSPACE_PATH}"
echo "[INFO] COMPOSE_ENV_FILE=${COMPOSE_ENV_FILE}"

"${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" config --quiet

build_target=false
case "${TARGET}" in
    hololive-api|hololive-alarm-worker|youtube-producer|youtube-producer-c|admin-dashboard)
        build_target=true
        ;;
    "")
        build_target=true
        ;;
esac

if [[ "${build_target}" == true ]]; then
    # producer Dockerfile은 빌드 컨텍스트를 ap-rsync 매니페스트로 프루닝하므로,
    # 매니페스트 누락은 원격 rsync뿐 아니라 이 로컬 빌드도 깨뜨린다.
    case "${TARGET}" in
        youtube-producer|youtube-producer-c|"")
            bash "${ROOT_DIR}/scripts/deploy/check-ap-rsync-manifest.sh"
            ;;
    esac
    if [[ -n "${TARGET}" ]]; then
        echo "[BUILD] ${TARGET}"
        "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" build "${TARGET}"
    else
        echo "[BUILD] all buildable services"
        "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" build
    fi
fi

if [[ -z "${TARGET}" ]] || cutover_service_uses_app_writable_bind_mount "${TARGET}"; then
    echo "[PREFLIGHT] Verifying host bind-mount write access for app uid ${HOLOLIVE_APP_UID}:${HOLOLIVE_APP_GID}"
    if ! cutover_bind_mount_preflight "${ROOT_DIR}"; then
        echo "[ERROR] host bind-mount preflight failed before cutover; aborting (no containers changed)" >&2
        exit 1
    fi
fi

if target_requires_db_migration; then
    run_db_migration_before_cutover
fi

if [[ "${TARGET}" == "hololive-api" || -z "${TARGET}" ]]; then
    removed_runtime_cleanup_before_cutover
fi

if [[ -n "${TARGET}" ]]; then
    cutover_capture_restart_baseline "${TARGET}"
    echo "[UP] ${TARGET}"
    up_args=(up -d)
    if [[ "${build_target}" == true ]]; then
        up_args+=(--no-build)
    fi
    up_args+=("${TARGET}")
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" "${up_args[@]}"
    echo "[PS] ${TARGET}"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" ps "${TARGET}"
    if ! cutover_health_gate "${TARGET}"; then
        echo "[ERROR] ${TARGET} failed health gate after redeploy" >&2
        exit 1
    fi
else
    cutover_capture_restart_baseline hololive-api hololive-alarm-worker
    echo "[UP] all services"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" up -d --no-build
    echo "[PS] all services"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE_ARGS[@]}" ps
    if ! cutover_health_gate hololive-api hololive-alarm-worker; then
        echo "[ERROR] health gate failed after all-service redeploy" >&2
        exit 1
    fi
fi
