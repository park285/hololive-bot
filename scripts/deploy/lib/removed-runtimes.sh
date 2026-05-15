#!/usr/bin/env bash

removed_runtime_cleanup_standalone_dispatcher() {
    local container_cli="${CONTAINER_CLI:-docker}"
    local container_name="hololive-dispatcher-go"
    local container_id

    container_id="$("${container_cli}" ps -aq --filter "name=^${container_name}$" 2>/dev/null || true)"
    if [[ -z "${container_id}" ]]; then
        return 0
    fi

    echo "[CLEANUP] Removing removed standalone dispatcher runtime: ${container_name}"
    "${container_cli}" stop "${container_name}" >/dev/null 2>&1 || true
    "${container_cli}" rm -f "${container_name}" >/dev/null
}
