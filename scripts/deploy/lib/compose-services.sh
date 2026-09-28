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

compose_service_resolve_redeploy_target() {
    local key="$1"

    case "${key}" in
        hololive-api) printf '%s\n' "hololive-api" ;;
        hololive-alarm-worker|alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        youtube-collector|youtube-collector-c) printf '%s\n' "youtube-collector" ;;
        youtube-po-c|po-sandbox) printf '%s\n' "youtube-po-c" ;;
        holo-postgres|postgres) printf '%s\n' "holo-postgres" ;;
        valkey-cache|valkey) printf '%s\n' "valkey-cache" ;;
        hololive-db-migrate|migrate) printf '%s\n' "hololive-db-migrate" ;;
        docker-proxy) printf '%s\n' "docker-proxy" ;;
        deunhealth) printf '%s\n' "deunhealth" ;;
        *) return 1 ;;
    esac
}

compose_service_redeploy_usage_lines() {
    printf '%s\n' \
        "  hololive-api" \
        "  hololive-alarm-worker | alarm-worker" \
        "  youtube-collector | youtube-collector-c (paired cutover)" \
        "  youtube-po-c | po-sandbox (paired cutover)" \
        "  holo-postgres | postgres" \
        "  valkey-cache | valkey" \
        "  hololive-db-migrate | migrate" \
        "  docker-proxy" \
        "  deunhealth"
}

compose_service_resolve_log_target() {
    local key="$1"

    case "${key}" in
        hololive-api) printf '%s\n' "hololive-api" ;;
        alarm-worker|hololive-alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        youtube-collector|youtube-collector-c) printf '%s\n' "youtube-collector" ;;
        *) return 1 ;;
    esac
}

compose_service_log_targets_text() {
    printf '%s\n' "hololive-api alarm-worker hololive-alarm-worker youtube-collector youtube-collector-c"
}
