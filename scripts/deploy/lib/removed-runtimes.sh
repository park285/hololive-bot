#!/usr/bin/env bash

removed_runtime_container_names() {
    printf '%s\n' \
        "admin-dashboard" \
        "hololive-kakao-bot-go" \
        "hololive-bot" \
        "hololive-admin-api" \
        "hololive-llm-scheduler" \
        "llm-scheduler" \
        "hololive-dispatcher-go" \
        "hololive-youtube-producer" \
        "hololive-youtube-producer-a" \
        "hololive-youtube-producer-b" \
        "hololive-youtube-producer-c" \
        "hololive-youtube-producer-d"
}

removed_runtime_cleanup_before_cutover() {
    local container_cli="${CONTAINER_CLI:-docker}"
    local container_name=""
    local container_id=""

    while IFS= read -r container_name; do
        [[ -n "${container_name}" ]] || continue
        if ! container_id="$("${container_cli}" ps -aq --filter "name=^${container_name}$")"; then
            echo "[ERROR] Cannot inspect retired runtime: ${container_name}" >&2
            return 1
        fi
        if [[ -z "${container_id}" ]]; then
            continue
        fi

        echo "[CUTOVER] Removing retired runtime container: ${container_name}"
        "${container_cli}" stop "${container_name}" >/dev/null 2>&1 || true
        "${container_cli}" rm -f "${container_name}" >/dev/null || return 1
    done < <(removed_runtime_container_names)
    removed_runtime_assert_absent
}

removed_runtime_assert_absent() {
    local container_name container_id
    while IFS= read -r container_name; do
        if ! container_id="$("${CONTAINER_CLI:-docker}" ps -aq --filter "name=^${container_name}$")"; then
            echo "[ERROR] Cannot verify retired runtime absence: ${container_name}" >&2
            return 1
        fi
        if [[ -n "${container_id}" ]]; then
            echo "[ERROR] Retired runtime still exists: ${container_name}" >&2
            return 1
        fi
    done < <(removed_runtime_container_names)
}
