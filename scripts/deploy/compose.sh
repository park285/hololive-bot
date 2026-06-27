#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
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

resolve_required_workspace_path() {
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
        return 1
    fi

    (cd "${candidate}" && pwd)
}

resolve_optional_workspace_path() {
    local explicit_value="$1"
    local sibling_path="$2"
    local embedded_path="$3"
    local label="$4"

    if [[ -n "${explicit_value}" ]]; then
        if [[ ! -d "${explicit_value}" ]]; then
            echo "[ERROR] Explicit ${label} workspace not found: ${explicit_value}" >&2
            return 1
        fi
        (cd "${explicit_value}" && pwd)
        return
    fi

    if [[ -d "${sibling_path}" ]]; then
        (cd "${sibling_path}" && pwd)
        return
    fi
    if [[ -d "${embedded_path}" ]]; then
        (cd "${embedded_path}" && pwd)
        return
    fi

    # Producer-only AP hosts do not need this build context. Keep the conventional
    # absolute candidate so Compose can render; an API image build will fail before
    # any runtime is stopped if the context is genuinely required and absent.
    printf '%s\n' "${sibling_path}"
}

compose_args=()
compose_files=()
compose_invokes_up=false
compose_up_build=false
previous=""
for arg in "$@"; do
    if [[ "${previous}" == "-f" || "${previous}" == "--file" ]]; then
        resolved_file="$(compose_file_resolve_path "${arg}")"
        compose_files+=("${resolved_file}")
        compose_args+=("${previous}" "${resolved_file}")
        previous=""
        continue
    fi

    case "${arg}" in
        -f|--file)
            previous="${arg}"
            continue
            ;;
        --file=*)
            resolved_file="$(compose_file_resolve_path "${arg#--file=}")"
            compose_files+=("${resolved_file}")
            compose_args+=("--file=${resolved_file}")
            continue
            ;;
        --env-file|--env-file=*)
            echo "[ERROR] Use COMPOSE_ENV_FILE with this wrapper; do not pass --env-file directly" >&2
            exit 1
            ;;
        up)
            compose_invokes_up=true
            ;;
        --build)
            if [[ "${compose_invokes_up}" == true ]]; then
                compose_up_build=true
                continue
            fi
            ;;
    esac

    compose_args+=("${arg}")
done

if [[ -n "${previous}" ]]; then
    echo "[ERROR] Missing value for ${previous}" >&2
    exit 1
fi

if [[ ${#compose_files[@]} -eq 0 ]]; then
    compose_files=(deploy/compose/docker-compose.prod.yml)
    compose_args=(-f deploy/compose/docker-compose.prod.yml "${compose_args[@]}")
fi

SHARED_GO_WORKSPACE_PATH="$(resolve_required_workspace_path \
    "${SHARED_GO_WORKSPACE_PATH:-}" \
    "${ROOT_DIR}/../shared-go" \
    "${ROOT_DIR}/shared-go" \
    "shared-go")"
IRIS_CLIENT_GO_WORKSPACE_PATH="$(resolve_optional_workspace_path \
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
        echo "        Allowed values: docker, podman" >&2
        exit 1
        ;;
esac

if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
    echo "[ERROR] Container CLI not found: ${CONTAINER_CLI}" >&2
    exit 1
fi

COMPOSE_CMD=("${CONTAINER_CLI}" compose)
if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=(podman-compose)
elif ! "${CONTAINER_CLI}" compose version >/dev/null 2>&1; then
    echo "[ERROR] '${CONTAINER_CLI} compose' is unavailable" >&2
    exit 1
fi

if ! COMPOSE_ENV_FILE="$(compose_env_resolve_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE

compose_env_validate_file_format "${COMPOSE_ENV_FILE}"
compose_env_assert_shell_matches_all_file_keys "${COMPOSE_ENV_FILE}"
compose_env_assert_no_shell_shadow_for_compose_files "${COMPOSE_ENV_FILE}" "${compose_files[@]}"
compose_env_assert_admin_dashboard_loopback_bind "${COMPOSE_ENV_FILE}"

if [[ "${compose_invokes_up}" == true ]]; then
    compose_env_assert_live_compat_for_host_networked_postgres "${compose_files[@]}"

    up_index=-1
    for index in "${!compose_args[@]}"; do
        if [[ "${compose_args[$index]}" == "up" ]]; then
            up_index="${index}"
            break
        fi
    done
    if (( up_index < 0 )); then
        echo "[ERROR] Internal error: compose up index was not found" >&2
        exit 1
    fi

    compose_prefix=("${compose_args[@]:0:up_index}")
    up_service_targets=()
    option_requires_value=false
    after_separator=false
    for ((index = up_index + 1; index < ${#compose_args[@]}; index++)); do
        token="${compose_args[$index]}"
        if [[ "${option_requires_value}" == true ]]; then
            option_requires_value=false
            continue
        fi
        if [[ "${after_separator}" == true ]]; then
            up_service_targets+=("${token}")
            continue
        fi
        case "${token}" in
            --)
                after_separator=true
                ;;
            --scale|--wait-timeout|--timeout|-t|--exit-code-from|--pull|--attach|--no-attach)
                option_requires_value=true
                ;;
            --scale=*|--wait-timeout=*|--timeout=*|--exit-code-from=*|--pull=*|--attach=*|--no-attach=*)
                ;;
            -*)
                ;;
            *)
                up_service_targets+=("${token}")
                ;;
        esac
    done

    cutover_required=false
    if [[ ${#up_service_targets[@]} -eq 0 ]]; then
        cutover_required=true
    else
        for service in "${up_service_targets[@]}"; do
            case "${service}" in
                hololive-api|admin-dashboard)
                    cutover_required=true
                    ;;
            esac
        done
    fi

    echo "[PREFLIGHT] Rendering Compose before start"
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_prefix[@]}" config --quiet

    if [[ "${compose_up_build}" == true ]]; then
        echo "[PREFLIGHT] Building images before start"
        if [[ ${#up_service_targets[@]} -gt 0 ]]; then
            "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" \
                "${compose_prefix[@]}" build --with-dependencies "${up_service_targets[@]}"
        else
            "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_prefix[@]}" build
        fi
    fi

    if [[ "${cutover_required}" == true ]]; then
        COMPOSE_FILE_ARGS=("${compose_prefix[@]}")
        gate_targets=()
        if [[ ${#up_service_targets[@]} -eq 0 ]]; then
            gate_targets=(hololive-api hololive-alarm-worker admin-dashboard)
        else
            for service in "${up_service_targets[@]}"; do
                case "${service}" in
                    hololive-api|hololive-alarm-worker|admin-dashboard)
                        gate_targets+=("${service}")
                        ;;
                esac
            done
        fi
        echo "[PREFLIGHT] Verifying host bind-mount write access for app uid ${HOLOLIVE_APP_UID}:${HOLOLIVE_APP_GID}"
        if ! cutover_bind_mount_preflight "${ROOT_DIR}"; then
            echo "[ERROR] host bind-mount preflight failed before cutover; aborting (no containers changed)" >&2
            exit 1
        fi
        removed_runtime_cleanup_before_cutover
        if [[ ${#gate_targets[@]} -gt 0 ]]; then
            cutover_capture_restart_baseline "${gate_targets[@]}"
        fi
    fi

    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"

    if [[ "${cutover_required}" == true ]]; then
        if [[ ${#gate_targets[@]} -gt 0 ]] && ! cutover_health_gate "${gate_targets[@]}"; then
            echo "[ERROR] health gate failed after cutover up" >&2
            exit 1
        fi
    fi
    exit 0
fi

exec "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"
