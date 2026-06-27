#!/usr/bin/env bash

removed_runtime_container_names() {
    printf '%s\n' \
        "hololive-kakao-bot-go" \
        "hololive-bot" \
        "hololive-admin-api" \
        "hololive-llm-scheduler" \
        "llm-scheduler" \
        "hololive-dispatcher-go"
}

removed_runtime_cleanup_before_cutover() {
    local container_cli="${CONTAINER_CLI:-docker}"
    local container_name=""
    local container_id=""

    while IFS= read -r container_name; do
        [[ -n "${container_name}" ]] || continue
        container_id="$("${container_cli}" ps -aq --filter "name=^${container_name}$" 2>/dev/null || true)"
        if [[ -z "${container_id}" ]]; then
            continue
        fi

        echo "[CUTOVER] Removing retired runtime container: ${container_name}"
        "${container_cli}" stop "${container_name}" >/dev/null 2>&1 || true
        "${container_cli}" rm -f "${container_name}" >/dev/null
    done < <(removed_runtime_container_names)
}
