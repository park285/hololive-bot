#!/usr/bin/env bash

compose_service_resolve_build_target() {
    local key="$1"

    case "${key}" in
        bot|hololive-bot|hololive-kakao-bot-go) printf '%s\n' "hololive-bot" ;;
        admin-api|hololive-admin-api) printf '%s\n' "hololive-admin-api" ;;
        alarm-worker|hololive-alarm-worker) printf '%s\n' "hololive-alarm-worker" ;;
        youtube-producer) printf '%s\n' "youtube-producer" ;;
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
        "youtube-producer" \
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
        youtube-producer) printf '%s\n' "youtube-producer" ;;
        youtube-producer-c) printf '%s\n' "youtube-producer-c" ;;
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
        "  youtube-producer" \
        "  youtube-producer-c (main-ap; COMPOSE_FILE 에 deploy/compose/docker-compose.main-ap.yml + COMPOSE_PROFILES=main-ap 필요)" \
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
        youtube-producer) printf '%s\n' "youtube-producer" ;;
        youtube-producer-c) printf '%s\n' "youtube-producer-c" ;;
        llm|llm-scheduler) printf '%s\n' "llm-scheduler" ;;
        *) return 1 ;;
    esac
}

compose_service_log_targets_text() {
    printf '%s\n' "bot hololive-bot youtube-producer youtube-producer-c llm llm-scheduler"
}
