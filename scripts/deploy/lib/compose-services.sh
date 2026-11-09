#!/usr/bin/env bash

compose_service_resolve_build_target() {
    local key="$1"

    case "${key}" in
        hololive-api|hololive-alarm-worker|youtube-collector|youtube-po-c) printf '%s\n' "${key}" ;;
        *) return 1 ;;
    esac
}

compose_service_build_targets_text() {
    printf '%s\n' \
        "hololive-api" \
        "hololive-alarm-worker" \
        "youtube-collector" \
        "youtube-po-c"
}

compose_service_resolve_log_target() {
    local key="$1"

    case "${key}" in
        hololive-api|hololive-alarm-worker) printf '%s\n' "${key}" ;;
        youtube-collector) printf '%s\n' "hololive-youtube-collector-c" ;;
        *) return 1 ;;
    esac
}

compose_service_log_targets_text() {
    printf '%s\n' "hololive-api hololive-alarm-worker youtube-collector"
}
