#!/usr/bin/env bash

compose_env_resolve_file() {
    if [[ -n "${COMPOSE_ENV_FILE:-}" ]]; then
        if [[ ! -r "${COMPOSE_ENV_FILE}" ]]; then
            echo "[ERROR] COMPOSE_ENV_FILE not readable: ${COMPOSE_ENV_FILE}" >&2
            exit 1
        fi
        realpath -e -- "${COMPOSE_ENV_FILE}"
        return
    fi

    local openbao_env="${OPENBAO_HOLOLIVE_ENV_FILE:-/run/hololive-bot/compose.env}"
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
    elif [[ "${value}" == \'* && "${value}" == *\' ]]; then
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
        if [[ -z "${key}" ]]; then
            continue
        fi
        if [[ -z "${!key+x}" ]]; then
            continue
        fi
        shell_value="${!key}"
        file_value="$(compose_env_read_value_from_file "${env_file}" "${key}")"
        if [[ "${shell_value}" != "${file_value}" ]]; then
            echo "[ERROR] Shell env ${key} differs from ${env_file}; unset it or update the env file" >&2
            exit 1
        fi
    done < <(compose_env_list_keys_from_file "${env_file}")
}

compose_env_list_interpolation_keys_from_files() {
    awk '
        {
            line = $0
            while (match(line, /\$\{[A-Za-z_][A-Za-z0-9_]*/)) {
                key = substr(line, RSTART + 2, RLENGTH - 2)
                print key
                line = substr(line, RSTART + RLENGTH)
            }
        }
    ' "$@" | sort -u
}

compose_env_key_exists_in_file() {
    local env_file="$1"
    local want="$2"

    compose_env_list_keys_from_file "${env_file}" | grep -qx -- "${want}"
}

compose_env_is_allowed_shell_control_key() {
    local key="$1"

    case "${key}" in
        COMPOSE_ENV_FILE|OPENBAO_HOLOLIVE_ENV_FILE|COMPOSE_PROFILES|COMPOSE_PROJECT_NAME)
            return 0
            ;;
        HOLOLIVE_BOT_ENV_FILE|HOLOLIVE_ALARM_WORKER_ENV_FILE|HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE)
            return 0
            ;;
        SHARED_GO_WORKSPACE_PATH|IRIS_CLIENT_GO_WORKSPACE_PATH)
            return 0
            ;;
        HOLO_API_VERSION|HOLO_BOT_VERSION|HOLO_ALARM_WORKER_VERSION)
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
        if [[ -z "${key}" ]]; then
            continue
        fi
        if compose_env_is_allowed_shell_control_key "${key}"; then
            continue
        fi
        if [[ -z "${!key+x}" ]]; then
            continue
        fi
        if ! compose_env_key_exists_in_file "${env_file}" "${key}"; then
            echo "[ERROR] Shell env ${key} would override Compose interpolation but is not present in ${env_file}" >&2
            echo "        Move ${key} into ${env_file}, or unset it before deploy." >&2
            exit 1
        fi

        shell_value="${!key}"
        file_value="$(compose_env_read_value_from_file "${env_file}" "${key}")"
        if [[ "${shell_value}" != "${file_value}" ]]; then
            echo "[ERROR] Shell env ${key} differs from ${env_file}; unset it or update the env file" >&2
            exit 1
        fi
    done < <(compose_env_list_interpolation_keys_from_files "$@")
}

compose_env_admin_dashboard_bind_ip() {
    local env_file="$1"
    local value=""

    if [[ -n "${ADMIN_DASHBOARD_PORT_BIND_IP+x}" ]]; then
        printf '%s\n' "${ADMIN_DASHBOARD_PORT_BIND_IP}"
        return
    fi

    if [[ -n "${env_file}" && -r "${env_file}" ]] \
       && compose_env_key_exists_in_file "${env_file}" "ADMIN_DASHBOARD_PORT_BIND_IP"; then
        value="$(compose_env_read_value_from_file "${env_file}" "ADMIN_DASHBOARD_PORT_BIND_IP")"
        printf '%s\n' "${value}"
        return
    fi

    printf '%s\n' "127.0.0.1"
}

compose_env_assert_admin_dashboard_loopback_bind() {
    local env_file="${1:-}"
    local bind_ip=""
    bind_ip="$(compose_env_admin_dashboard_bind_ip "${env_file}")"

    case "${bind_ip}" in
        127.0.0.1|::1|"")
            return 0
            ;;
    esac

    echo "[ERROR] ADMIN_DASHBOARD_PORT_BIND_IP must bind admin-dashboard to loopback" >&2
    echo "        (127.0.0.1 or ::1), got: ${bind_ip}." >&2
    echo "        The dashboard has no edge auth in front of the bind; expose it via a" >&2
    echo "        reverse proxy on loopback instead of binding to a routable address." >&2
    exit 1
}

compose_postgres_runtime_network_mode() {
    local cli="${CONTAINER_CLI:-docker}"
    "${cli}" inspect holo-postgres --format '{{.HostConfig.NetworkMode}}' 2>/dev/null || true
}

# holo-postgres가 host network(live-compat 토폴로지)로 떠 있을 때 live-compat overlay 없이
# 배포하면 bridge로 재생성되어 host:5433 소비자(AP youtube-producer 등)의 DB 연결이 끊긴다.
# 의도치 않은 토폴로지 변경을 fail-closed로 막는다.
compose_env_assert_live_compat_for_host_networked_postgres() {
    local path
    for path in "$@"; do
        case "${path##*/}" in
            docker-compose.live-compat.yml) return 0 ;;
        esac
    done

    local pg_net
    pg_net="$(compose_postgres_runtime_network_mode)"
    if [[ "${pg_net}" != "host" ]]; then
        return 0
    fi

    if [[ "${ALLOW_POSTGRES_TOPOLOGY_CHANGE:-}" == "true" ]]; then
        echo "[WARN] holo-postgres is host-networked but no live-compat overlay is set;" >&2
        echo "       proceeding because ALLOW_POSTGRES_TOPOLOGY_CHANGE=true." >&2
        return 0
    fi

    echo "[ERROR] holo-postgres runs on host network (live-compat topology) but COMPOSE_FILE" >&2
    echo "        has no live-compat overlay. Deploying now would recreate holo-postgres on a" >&2
    echo "        bridge network and break host:5433 consumers (AP youtube-producer-a/b 등)." >&2
    echo "        Add the overlay, for example:" >&2
    echo "          COMPOSE_FILE=deploy/compose/docker-compose.prod.yml:deploy/compose/docker-compose.live-compat.yml" >&2
    echo "        Set ALLOW_POSTGRES_TOPOLOGY_CHANGE=true only for an intentional topology change." >&2
    exit 1
}
