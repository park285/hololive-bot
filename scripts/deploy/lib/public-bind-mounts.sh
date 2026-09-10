#!/usr/bin/env bash

is_admin_ingress_ipv4() {
    local value="$1" octet
    local -a octets

    [[ "${value}" =~ ^[0-9]+(\.[0-9]+){3}$ ]] || return 1
    IFS=. read -r -a octets <<<"${value}"
    [[ "${#octets[@]}" -eq 4 ]] || return 1

    for octet in "${octets[@]}"; do
        [[ "${octet}" =~ ^[0-9]{1,3}$ ]] && (( 10#${octet} <= 255 )) || return 1
    done
}

prepare_admin_dashboard_ingress_bind_mount() {
    local root="$1"
    local template="${root}/deploy/nginx/admin-dashboard-ingress.conf.template"
    local config="${HOLOLIVE_INGRESS_CONF:-/run/hololive-bot/admin-dashboard-ingress.conf}"
    local bind_ip="${HOLOLIVE_BOT_PORT_BIND_IP:-}"
    local maintenance="${config}.maintenance" guard=""

    if [[ -z "${bind_ip}" && -n "${COMPOSE_ENV_FILE:-}" && -r "${COMPOSE_ENV_FILE}" ]]; then
        bind_ip="$(sed -n 's/^HOLOLIVE_BOT_PORT_BIND_IP=[[:space:]]*//p' "${COMPOSE_ENV_FILE}" | head -1)"
    fi
    if [[ -z "${bind_ip}" ]]; then
        echo "[PREFLIGHT] HOLOLIVE_BOT_PORT_BIND_IP is required to render the public ingress config" >&2
        return 1
    fi
    if ! is_admin_ingress_ipv4 "${bind_ip}"; then
        echo "[PREFLIGHT] HOLOLIVE_BOT_PORT_BIND_IP must be a literal IPv4 address" >&2
        return 1
    fi
    if [[ -L "${template}" || ! -f "${template}" ]]; then
        echo "[PREFLIGHT] public ingress template must be a regular file: ${template}" >&2
        return 1
    fi
    if ! install -d -m 0755 -- "$(dirname -- "${config}")"; then
        echo "[PREFLIGHT] could not create public ingress config directory: ${config}" >&2
        return 1
    fi
    if [[ -L "${config}" ]]; then
        echo "[PREFLIGHT] public ingress config must not be a symlink: ${config}" >&2
        return 1
    fi
    if [[ -e "${config}" && ! -f "${config}" ]]; then
        echo "[PREFLIGHT] public ingress config must be a regular file: ${config}" >&2
        return 1
    fi
    # 정비 marker는 후속 compose preflight가 관리자 origin을 다시 열지 못하게 유지합니다.
    if [[ -e "${maintenance}" || -L "${maintenance}" ]]; then
        if [[ -L "${maintenance}" || ! -f "${maintenance}" \
            || "$(stat -c '%u' -- "${maintenance}")" != "$(id -u)" \
            || "$(stat -c '%a' -- "${maintenance}")" != "600" \
            || "$(cat -- "${maintenance}")" != "closed" ]]; then
            echo "[PREFLIGHT] invalid administrator maintenance marker" >&2
            return 1
        fi
        guard='return 503;'
    fi
    if ! sed -e "s/@BIND_IP@/${bind_ip}/g" -e "s/@ADMIN_MAINTENANCE@/${guard}/g" -- "${template}" >"${config}"; then
        echo "[PREFLIGHT] could not render public ingress config: ${config}" >&2
        return 1
    fi
    if ! chmod 0644 -- "${config}"; then
        echo "[PREFLIGHT] could not set public ingress config mode to 0644: ${config}" >&2
        return 1
    fi
    if [[ "$(stat -c '%a' "${config}")" != "644" ]]; then
        echo "[PREFLIGHT] public ingress config mode is not 0644: ${config}" >&2
        return 1
    fi
    if grep -Eq '@(BIND_IP|ADMIN_MAINTENANCE)@' -- "${config}"; then
        echo "[PREFLIGHT] public ingress config still contains an unrendered placeholder: ${config}" >&2
        return 1
    fi
    export HOLOLIVE_INGRESS_CONF="${config}"
}
