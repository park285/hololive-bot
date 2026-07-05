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

    local openbao_env="${OPENBAO_HOLOLIVE_ENV_FILE:-/run/hololive-bot/compose.env}"
    if [[ -r "${openbao_env}" ]]; then
        printf '%s\n' "${openbao_env}"
        return
    fi

    echo "[ERROR] Compose env file not readable: ${openbao_env}" >&2
    echo "        Set COMPOSE_ENV_FILE explicitly for non-OpenBao or test deployments." >&2
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
        HOLOLIVE_API_ENV_FILE|HOLOLIVE_ALARM_WORKER_ENV_FILE|HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE|ADMIN_DASHBOARD_ENV_FILE)
            return 0
            ;;
        SHARED_GO_WORKSPACE_PATH|IRIS_CLIENT_GO_WORKSPACE_PATH)
            return 0
            ;;
        HOLO_API_VERSION|HOLO_ALARM_WORKER_VERSION)
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
    return 1
}

compose_postgres_runtime_network_mode() {
    local cli="${CONTAINER_CLI:-docker}"
    "${cli}" inspect holo-postgres --format '{{.HostConfig.NetworkMode}}' 2>/dev/null || true
}

compose_postgres_runtime_published_host_ips() {
    local cli="${CONTAINER_CLI:-docker}"
    "${cli}" inspect holo-postgres \
        --format '{{range $p, $conf := .NetworkSettings.Ports}}{{range $conf}}{{.HostIp}}{{println}}{{end}}{{end}}' \
        2>/dev/null || true
}

compose_env_assert_live_compat_for_host_networked_postgres() {
    local path=""
    for path in "$@"; do
        case "${path##*/}" in
            docker-compose.live-compat.yml)
                return 0
                ;;
        esac
    done

    # base prod.yml 은 holo-postgres 를 publish 하지 않으므로, host-network 이거나 non-loopback 으로
    # publish 돼 있으면 live-compat overlay 가 활성이라는 신호다(2026-06-27·2026-06-29 사건).
    local live_compat_active=""
    if [[ "$(compose_postgres_runtime_network_mode)" == "host" ]]; then
        live_compat_active="host-networked holo-postgres"
    else
        local ip=""
        while IFS= read -r ip; do
            case "${ip}" in
                ""|127.0.0.1|::1) ;;
                *)
                    live_compat_active="holo-postgres published on ${ip}"
                    break
                    ;;
            esac
        done < <(compose_postgres_runtime_published_host_ips)
    fi

    if [[ -z "${live_compat_active}" ]]; then
        return 0
    fi

    if [[ "${ALLOW_POSTGRES_TOPOLOGY_CHANGE:-}" == "true" ]]; then
        echo "[WARN] live-compat topology active (${live_compat_active}) but no live-compat overlay is set;" >&2
        echo "       proceeding because ALLOW_POSTGRES_TOPOLOGY_CHANGE=true." >&2
        return 0
    fi

    echo "[ERROR] live-compat topology is active (${live_compat_active}) but COMPOSE_FILE has no" >&2
    echo "        live-compat overlay. Deploying now would recreate holo-postgres/valkey on loopback-only" >&2
    echo "        bindings and drop AP (osaka/seoul/osaka2) off 100.100.1.3 valkey/postgres." >&2
    echo "        Add deploy/compose/docker-compose.live-compat.yml, or set" >&2
    echo "        ALLOW_POSTGRES_TOPOLOGY_CHANGE=true only for an intentional topology change." >&2
    return 1
}
