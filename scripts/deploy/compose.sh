#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"

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

compose_args=("$@")
compose_files=()
previous=""
for arg in "$@"; do
    if [[ "${previous}" == "-f" || "${previous}" == "--file" ]]; then
        compose_files+=("${arg}")
        previous=""
        continue
    fi

    case "${arg}" in
        -f|--file)
            previous="${arg}"
            ;;
        --file=*)
            compose_files+=("${arg#--file=}")
            ;;
        --env-file|--env-file=*)
            echo "[ERROR] Use COMPOSE_ENV_FILE with this wrapper; do not pass --env-file directly" >&2
            exit 1
            ;;
    esac
done

if [[ -n "${previous}" ]]; then
    echo "[ERROR] Missing value for ${previous}" >&2
    exit 1
fi

if [[ ${#compose_files[@]} -eq 0 ]]; then
    compose_files=(docker-compose.prod.yml)
    compose_args=(-f docker-compose.prod.yml "$@")
fi

if ! COMPOSE_ENV_FILE="$(compose_env_resolve_file)"; then
    exit 1
fi
export COMPOSE_ENV_FILE

compose_env_validate_file_format "${COMPOSE_ENV_FILE}"
compose_env_assert_shell_matches_all_file_keys "${COMPOSE_ENV_FILE}"
compose_env_assert_no_shell_shadow_for_compose_files "${COMPOSE_ENV_FILE}" "${compose_files[@]}"

exec "${COMPOSE_CMD[@]}" --env-file "${COMPOSE_ENV_FILE}" "${compose_args[@]}"
