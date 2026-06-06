#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
. "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh"

compose_file_resolve_path() {
    local file="$1"
    if [[ ! -r "${file}" && -r "${ROOT_DIR}/deploy/compose/${file}" ]]; then
        printf '%s\n' "deploy/compose/${file}"
        return
    fi
    printf '%s\n' "${file}"
}

resolve_shared_go_workspace_path() {
    local candidate="${SHARED_GO_WORKSPACE_PATH:-}"
    if [[ -z "${candidate}" ]]; then
        if [[ -d "${ROOT_DIR}/../shared-go" ]]; then
            candidate="${ROOT_DIR}/../shared-go"
        elif [[ -d "${ROOT_DIR}/shared-go" ]]; then
            candidate="${ROOT_DIR}/shared-go"
        fi
    fi
    if [[ ! -d "${candidate}" ]]; then
        echo "[ERROR] Active shared-go workspace not found" >&2
        exit 1
    fi

    printf '%s\n' "$(cd "${candidate}" && pwd)"
}

if ! SHARED_GO_WORKSPACE_PATH="$(resolve_shared_go_workspace_path)"; then
    exit 1
fi
export SHARED_GO_WORKSPACE_PATH

compose_args=()
compose_files=()
compose_invokes_up=false
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
    esac

    if [[ "${arg}" == "up" ]]; then
        compose_invokes_up=true
    fi
    compose_args+=("${arg}")
done

if [[ -n "${previous}" ]]; then
    echo "[ERROR] Missing value for ${previous}" >&2
    exit 1
fi

if [[ ${#compose_files[@]} -eq 0 ]]; then
    compose_files=(deploy/compose/docker-compose.prod.yml)
    compose_args=(-f deploy/compose/docker-compose.prod.yml "$@")
fi

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

COMPOSE_CMD=("${CONTAINER_CLI}" "compose")
if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
    COMPOSE_CMD=("podman-compose")
elif ! "${CONTAINER_CLI}" compose version >/dev/null 2>&1; then
    if [[ "${CONTAINER_CLI}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
        COMPOSE_CMD=("podman-compose")
    else
        echo "[ERROR] '${CONTAINER_CLI} compose' is unavailable" >&2
        exit 1
    fi
fi

if ! COMPOSE_ENV_FILE="$(compose_env_resolve_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE

compose_env_validate_file_format "${COMPOSE_ENV_FILE}"
compose_env_assert_shell_matches_all_file_keys "${COMPOSE_ENV_FILE}"
compose_env_assert_no_shell_shadow_for_compose_files "${COMPOSE_ENV_FILE}" "${compose_files[@]}"

if [[ "${compose_invokes_up}" == true ]]; then
    "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"
    removed_runtime_cleanup_standalone_dispatcher
    exit 0
fi

exec "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"
