#!/usr/bin/env bash

compose_service_resolve_build_target() {
    local key="$1"

    case "${key}" in
        hololive-api) printf '%s\n' "hololive-api" ;;
        hololive-alarm-worker|alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        youtube-producer) printf '%s\n' "youtube-producer" ;;
        admin-dashboard) printf '%s\n' "admin-dashboard" ;;
        *) return 1 ;;
    esac
}

compose_service_build_targets_text() {
    printf '%s\n' \
        "hololive-api" \
        "hololive-alarm-worker alarm-worker" \
        "youtube-producer" \
        "admin-dashboard"
}

compose_service_resolve_redeploy_target() {
    local key="$1"

    case "${key}" in
        hololive-api) printf '%s\n' "hololive-api" ;;
        hololive-alarm-worker|alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        youtube-producer) printf '%s\n' "youtube-producer" ;;
        youtube-producer-c) printf '%s\n' "youtube-pro