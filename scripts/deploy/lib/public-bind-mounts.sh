#!/usr/bin/env bash

prepare_admin_dashboard_ingress_bind_mount() {
    local root="$1"
    local config="${root}/deploy/nginx/admin-dashboard-ingress.conf"

    if [[ -L "${config}" || ! -f "${config}" ]]; then
        echo "[PREFLIGHT] public ingress config must be a regular file: ${config}" >&2
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
}
