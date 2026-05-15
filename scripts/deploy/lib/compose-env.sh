#!/usr/bin/env bash

compose_env_resolve_file() {
    if [[ -n "${COMPOSE_ENV_FILE:-}" ]]; then
        if [[ ! -r "${COMPOSE_ENV_FILE}" ]]; then
            echo "[ERROR] COMPOSE_ENV_FILE not readable: ${COMPOSE_ENV_FILE}" >&2
            exit 1
        fi
        printf '%s\n' "${COMPOSE_ENV_FILE}"
        return
    fi

    local openbao_env="${OPENBAO_HOLOLIVE_ENV_FILE:-/run/hololive-bot/env}"
    if [[ -r "${openbao_env}" ]]; then
        printf '%s\n' "${openbao_env}"
        return
    fi

    echo "[ERROR] Compose env file not readable: ${openbao_env}" >&2
    echo "        Set COMPOSE_ENV_FILE explicitly for non-OpenBao or test deployments." >&2
    exit 1
}

compose_env_validate_file_format() {
    local env_file="$1"
    local export_line=""
    local command_sub_line=""

    export_line="$(awk '/^[[:space:]]*export[[:space:]]+/ { print NR; exit }' "${env_file}")"
    if [[ -n "${export_line}" ]]; then
        echo "[ERROR] Compose env file must not contain leading export: ${env_file}:${export_line}" >&2
        exit 1
    fi

    command_sub_line="$(awk '/[`]|[$][(]/ { print NR; exit }' "${env_file}")"
    if [[ -n "${command_sub_line}" ]]; then
        echo "[ERROR] Compose env file must not contain command substitution: ${env_file}:${command_sub_line}" >&2
        exit 1
    fi
}

compose_env_read_value_from_file() {
    local env_file="$1"
    local key="$2"
    local value=""

    value="$(awk -v k="${key}" '
        /^[[:space:]]*(#|$)/ { next }
        {
            line = $0
            sub(/\r$/, "", line)
            if (index(line, "=") == 0) {
                next
            }
            key = substr(line, 1, index(line, "=") - 1)
            sub(/^[[:space:]]+/, "", key)
            sub(/[[:space:]]+$/, "", key)
            if (key == k) {
                value = substr(line, index(line, "=") + 1)
            }
        }
        END { print value }
    ' "${env_file}")"

    value="${value%$'\r'}"
    if [[ "${value}" == \"*\" && "${value}" == *\" ]]; then
        value="${value#\"}"
        value="${value%\"}"
    elif [[ "${value}" == \'* && "${value}" == *\' ]]; then
        value="${value#\'}"
        value="${value%\'}"
    fi

    printf '%s\n' "${value}"
}

compose_env_assert_shell_matches_file() {
    local env_file="$1"
    shift
    local key=""
    local shell_value=""
    local file_value=""

    for key in "$@"; do
        if [[ -z "${!key+x}" ]]; then
            continue
        fi
        shell_value="${!key}"
        file_value="$(compose_env_read_value_from_file "${env_file}" "${key}")"
        if [[ "${shell_value}" != "${file_value}" ]]; then
            echo "[ERROR] Shell env ${key} differs from COMPOSE_ENV_FILE; unset it or update ${env_file}" >&2
            exit 1
        fi
    done
}
