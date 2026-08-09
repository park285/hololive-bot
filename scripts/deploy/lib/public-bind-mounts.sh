#!/usr/bin/env bash

prepare_admin_dashboard_ingress_bind_mount() {
    local root="$1"
    local template="${root}/deploy/nginx/admin-dashboard-ingress.conf.template"
    local config="${HOLOLIVE_INGRESS_CONF:-/run/hololive-bot/admin-dashboard-ingress.conf}"
    local bind_ip="${HOLOLIVE_BOT_PORT_BIND_IP:-}"

    if [[ -z "${bind_ip}" && -n "${COMPOSE_ENV_FILE:-}" && -r "${COMPOSE_ENV_FILE}" ]]; then
        bind_ip="$(sed -n 's/^HOLOLIVE_BOT_PORT_BIND_IP=[[:space:]]*//p' "${COMPOSE_ENV_FILE}" | head -1)"
    fi
    if [[ -z "${bind_ip}" ]]; then
        echo "[PREFLIGHT] HOLOLIVE_BOT_PORT_BIND_IP is required to render the public ingress config" >&2
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
    if ! sed "s/@BIND_IP@/${bind_ip}/g" -- "${template}" >"${config}"; then
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
    if grep -q '@BIND_IP@' -- "${config}"; then
        echo "[PREFLIGHT] public ingress config still contains an unrendered placeholder: ${config}" >&2
        return 1
    fi
    export HOLOLIVE_INGRESS_CONF="${config}"
}
