#!/usr/bin/env bash

compose_service_resolve_build_target() {
    local key="$1"

    case "${key}" in
        bot|hololive-bot|hololive-kakao-bot-go) printf '%s\n' "hololive-bot" ;;
        admin-api|hololive-admin-api) printf '%s\n' "hololive-admin-api" ;;
        alarm-worker|hololive-alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        stream-ingester) printf '%s\n' "stream-ingester" ;;
        youtube-scraper) printf '%s\n' "youtube-scraper" ;;
        llm-scheduler) printf '%s\n' "llm-scheduler" ;;
        admin-dashboard) printf '%s\n' "admin-dashboard" ;;
        *) return 1 ;;
    esac
}

compose_service_build_targets_text() {
    printf '%s\n' \
        "bot hololive-bot hololive-kakao-bot-go" \
        "admin-api hololive-admin-api" \
        "alarm-worker hololive-alarm-worker" \
        "stream-ingester" \
        "youtube-scraper" \
        "llm-scheduler" \
        "admin-dashboard"
}

compose_service_resolve_redeploy_target() {
    local key="$1"

    case "${key}" in
        hololive-bot|bot) printf '%s\n' "hololive-bot" ;;
        hololive-admin-api|admin-api) printf '%s\n' "hololive-admin-api" ;;
        hololive-alarm-worker|alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        llm-scheduler|llm) printf '%s\n' "llm-scheduler" ;;
        stream-ingester|ingester) printf '%s\n' "stream-ingester" ;;
        youtube-scraper|yt-scraper) printf '%s\n' "youtube-scraper" ;;
        holo-postgres|postgres) printf '%s\n' "holo-postgres" ;;
        valkey-cache|valkey) printf '%s\n' "valkey-cache" ;;
        hololive-db-migrate|migrate) printf '%s\n' "hololive-db-migrate" ;;
        docker-proxy) printf '%s\n' "docker-proxy" ;;
        admin-dashboard|admin) printf '%s\n' "admin-dashboard" ;;
        deunhealth) printf '%s\n' "deunhealth" ;;
        all) printf '%s\n' "" ;;
        *) return 1 ;;
    esac
}

compose_service_redeploy_usage_lines() {
    printf '%s\n' \
        "  hololive-bot | bot" \
        "  hololive-admin-api | admin-api" \
        "  hololive-alarm-worker | alarm-worker" \
        "  llm-scheduler | llm" \
        "  stream-ingester | ingester" \
        "  youtube-scraper | yt-scraper" \
        "  holo-postgres | postgres" \
        "  valkey-cache | valkey" \
        "  hololive-db-migrate | migrate" \
        "  docker-proxy" \
        "  admin-dashboard | admin" \
        "  deunhealth" \
        "  all"
}

compose_service_resolve_log_target() {
    local key="$1"

    case "${key}" in
        bot|hololive-bot) printf '%s\n' "hololive-bot" ;;
        ingester|stream-ingester) printf '%s\n' "stream-ingester" ;;
        llm|llm-scheduler) printf '%s\n' "llm-scheduler" ;;
        *) return 1 ;;
    esac
}

compose_service_log_targets_text() {
    printf '%s\n' "bot hololive-bot ingester stream-ingester llm llm-scheduler"
}

compose_service_resolve_osaka_log_targets() {
    local key="$1"

    case "${key}" in
        youtube|scraper|youtube-scraper) printf '%s\n' "youtube-scraper" ;;
        stream|ingester|stream-ingester) printf '%s\n' "stream-ingester" ;;
        all) printf '%s\n' "youtube-scraper" "stream-ingester" ;;
        *) return 1 ;;
    esac
}

compose_service_osaka_log_targets_text() {
    printf '%s\n' "youtube-scraper stream-ingester all"
}

compose_service_resolve_osaka_container() {
    local service="$1"

    case "${service}" in
        youtube-scraper) printf '%s\n' "hololive-youtube-scraper" ;;
        stream-ingester) printf '%s\n' "hololive-stream-ingester" ;;
        *) return 1 ;;
    esac
}

compose_service_resolve_osaka_log_file() {
    local service="$1"

    case "${service}" in
        youtube-scraper) printf '%s\n' "logs/youtube-scraper.log" ;;
        stream-ingester) printf '%s\n' "logs/stream-ingester.log" ;;
        *) return 1 ;;
    esac
}
