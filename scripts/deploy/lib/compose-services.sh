#!/usr/bin/env bash

# shellcheck source=scripts/deploy/lib/collector-slots.sh
. "$(dirname "${BASH_SOURCE[0]}")/collector-slots.sh"

compose_service_central_collector_slot() {
    collector_slots_where host central
}

compose_service_resolve_log_target() {
    local key="$1" slot unit

    case "${key}" in
        hololive-api|hololive-alarm-worker) printf '%s\n' "${key}" ;;
        *)
            slot="$(compose_service_central_collector_slot)" || return 1
            [[ -n "$slot" && "$(collector_slot "$slot" service)" == "$key" ]] || return 1
            unit="$(collector_slot "$slot" collector_unit)" || return 1
            printf '%s\n' "${unit%.service}"
            ;;
    esac
}

compose_service_log_targets_text() {
    printf '%s\n' "hololive-api hololive-alarm-worker $(collector_slot "$(compose_service_central_collector_slot)" service)"
}
