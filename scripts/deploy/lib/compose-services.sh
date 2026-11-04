#!/usr/bin/env bash

compose_service_resolve_build_target() {
    local key="$1"

    case "${key}" in
        hololive-api) printf '%s\n' "hololive-api" ;;
        alarm-worker|hololive-alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        youtube-collector|youtube-collector-c) printf '%s\n' "youtube-collector" ;;
        youtube-po-c|po-sandbox) printf '%s\n' "youtube-po-c" ;;
        *) return 1 ;;
    esac
}

compose_service_build_targets_text() {
    printf '%s\n' \
        "hololive-api" \
        "alarm-worker hololive-alarm-worker" \
        "youtube-collector" \
        "youtube-po-c | po-sandbox"
}

compose_service_resolve_log_target() {
    local key="$1"

    case "${key}" in
        hololive-api) printf '%s\n' "hololive-api" ;;
        alarm-worker|hololive-alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        youtube-collector|youtube-collector-c) printf '%s\n' "hololive-youtube-collector-c" ;;
        *) return 1 ;;
    esac
}

compose_service_log_targets_text() {
    printf '%s\n' "hololive-api alarm-worker hololive-alarm-worker youtube-collector youtube-collector-c"
}
