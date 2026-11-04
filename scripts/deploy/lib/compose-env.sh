#!/usr/bin/env bash

compose_env_resolve_file() {
    if [[ -n "${COMPOSE_ENV_FILE:-}" ]]; then
        if [[ ! -r "${COMPOSE_ENV_FILE}" ]]; then
            echo "[ERROR] COMPOSE_ENV_FILE not readable: ${COMPOSE_ENV_FILE}" >&2
            return 1
        fi
        realpath -e -- "${COMPOSE_ENV_FILE}"
        return
    fi

    local stack_secrets_env="${STACK_SECRETS_HOLOLIVE_ENV_FILE:-/etc/stack-secrets/hololive-bot/compose.env}"
    if [[ -r "${stack_secrets_env}" ]]; then
        printf '%s\n' "${stack_secrets_env}"
        return
    fi

    echo "[ERROR] Compose env file not readable: ${stack_secrets_env}" >&2
    echo "        Set COMPOSE_ENV_FILE explicitly for test or non-stack-secrets deployments." >&2
    return 1
}

compose_env_validate_file_format() {
    local env_file="$1"

    awk '
        /^[[:space:]]*(#|$)/ { next }

        /^[[:space:]]*export[[:space:]]+/ {
            print "[ERROR] Compose env file must not contain leading export: " FILENAME ":" NR > "/dev/stderr"
            exit 1
        }

        /[`]|[$][(]/ {
            print "[ERROR] Compose env file must not contain command substitution: " FILENAME ":" NR > "/dev/stderr"
            exit 1
        }

        index($0, "=") == 0 {
            print "[ERROR] Compose env file line must be KEY=VALUE: " FILENAME ":" NR > "/dev/stderr"
            exit 1
        }

        {
            line = $0
            sub(/\r$/, "", line)
            if (line ~ /[[:cntrl:]]/) {
                print "[ERROR] Compose env file line must not contain control characters: " FILENAME ":" NR > "/dev/stderr"
                exit 1
            }

            key = substr(line, 1, index(line, "=") - 1)
            raw_key = key
            sub(/^[[:space:]]+/, "", key)
            sub(/[[:space:]]+$/, "", key)

            if (key != raw_key) {
                print "[ERROR] Env key must not contain surrounding whitespace: " FILENAME ":" NR ":" raw_key > "/dev/stderr"
                exit 1
            }
            if (key !~ /^[A-Za-z_][A-Za-z0-9_]*$/) {
                print "[ERROR] Invalid env key: " FILENAME ":" NR ":" key > "/dev/stderr"
                exit 1
            }
            if (seen[key]++) {
                print "[ERROR] Duplicate env key: " FILENAME ":" NR ":" key > "/dev/stderr"
                exit 1
            }
        }
    ' "${env_file}"
}

compose_env_list_keys_from_file() {
    local env_file="$1"

    awk '
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
            if (key != "") {
                print key
            }
        }
    ' "${env_file}" | sort -u
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
    elif [[ "${value}" == \'*\' && "${value}" == *\' ]]; then
        value="${value#\'}"
        value="${value%\'}"
    fi

    printf '%s\n' "${value}"
}

compose_env_assert_shell_matches_all_file_keys() {
    local env_file="$1"
    local key=""
    local shell_value=""
    local file_value=""

    while IFS= read -r key; do
        [[ -n "${key}" ]] || continue
        [[ -n "${!key+x}" ]] || continue

        shell_value="${!key}"
        file_value="$(compose_env_read_value_from_file "${env_file}" "${key}")"
        if [[ "${shell_value}" != "${file_value}" ]]; then
            echo "[ERROR] Shell env ${key} differs from ${env_file}; unset it or update the env file" >&2
            return 1
        fi
    done < <(compose_env_list_keys_from_file "${env_file}")
}

compose_env_list_interpolation_keys_from_files() {
    local file collector_base
    local -a inputs=("$@")
    # 수집기 기본 파일은 AP의 -f가 아닌 extends로 읽는다.
    # central-c 서비스를 선택하지 않고 치환 키만 포함한다.
    for file in "$@"; do
        case "${file##*/}" in
            docker-compose.seoul.yml)
                collector_base="$(dirname "$file")/docker-compose.collector.yml"
                [[ ! -f "$collector_base" ]] || inputs+=("$collector_base")
                ;;
        esac
    done
    awk '
        {
            line = $0
            while (match(line, /\$\{[A-Za-z_][A-Za-z0-9_]*/)) {
                key = substr(line, RSTART + 2, RLENGTH - 2)
                print key
                line = substr(line, RSTART + RLENGTH)
            }
        }
    ' "${inputs[@]}" | sort -u
}

compose_env_key_exists_in_file() {
    local env_file="$1"
    local want="$2"
    compose_env_list_keys_from_file "${env_file}" | grep -qx -- "${want}"
}

compose_env_is_allowed_shell_control_key() {
    local key="$1"

    case "${key}" in
        COMPOSE_ENV_FILE|STACK_SECRETS_HOLOLIVE_ENV_FILE|COMPOSE_PROFILES|COMPOSE_PROJECT_NAME)
            return 0
            ;;
        HOLOLIVE_API_ENV_FILE|HOLOLIVE_ALARM_WORKER_ENV_FILE|HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE)
            return 0
            ;;
        HOLO_API_VERSION|HOLO_ALARM_WORKER_VERSION|REVISION)
            return 0
            ;;
        REMOTE_CACHE_PREFIX)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

compose_env_assert_no_shell_shadow_for_compose_files() {
    local env_file="$1"
    shift
    local key=""
    local shell_value=""
    local file_value=""

    while IFS= read -r key; do
        [[ -n "${key}" ]] || continue
        if compose_env_is_allowed_shell_control_key "${key}"; then
            continue
        fi
        [[ -n "${!key+x}" ]] || continue

        if ! compose_env_key_exists_in_file "${env_file}" "${key}"; then
            echo "[ERROR] Shell env ${key} would override Compose interpolation but is not present in ${env_file}" >&2
            echo "        Move ${key} into ${env_file}, or unset it before deploy." >&2
            return 1
        fi

        shell_value="${!key}"
        file_value="$(compose_env_read_value_from_file "${env_file}" "${key}")"
        if [[ "${shell_value}" != "${file_value}" ]]; then
            echo "[ERROR] Shell env ${key} differs from ${env_file}; unset it or update the env file" >&2
            return 1
        fi
    done < <(compose_env_list_interpolation_keys_from_files "$@")
}
