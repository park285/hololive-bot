#!/usr/bin/env bash

ap_host_usage_names() {
    local repo_root="$1"
    find "$repo_root/scripts/deploy/ap-hosts" -maxdepth 1 -name '*.conf' -printf '%f\n' 2>/dev/null | sed 's/\.conf$//' | sort | tr '\n' ' '
}

ap_host_load() {
    local repo_root="$1"
    local host="${2:-}"

    if [[ -z "$host" ]]; then
        echo "AP host name required. Available: $(ap_host_usage_names "$repo_root")" >&2
        return 2
    fi

    case "$host" in
        */*|*..*)
            echo "Refusing suspicious AP host name: $host" >&2
            return 2
            ;;
    esac

    local conf="$repo_root/scripts/deploy/ap-hosts/$host.conf"
    if [[ ! -r "$conf" ]]; then
        echo "Unknown AP host: $host (no $conf). Available: $(ap_host_usage_names "$repo_root")" >&2
        return 2
    fi

    AP_NAME=""
    AP_SSH_HOST=""
    AP_SSH_HOST_KEY_ALIAS=""
    AP_RUNTIME_MODE=""
    AP_SLOTS=()
    AP_SERVICES=()
    AP_PORTS=()
    AP_APPROVE_DEPLOY_VAR=""
    AP_APPROVE_ROLLBACK_VAR=""
    AP_BACKUP_PREFIX=""
    AP_GOAMD64=""

    # shellcheck disable=SC1090
    . "$conf"

    : "${AP_NAME:?missing AP_NAME in $conf}"
    : "${AP_SSH_HOST:?missing AP_SSH_HOST in $conf}"
    : "${AP_RUNTIME_MODE:?missing AP_RUNTIME_MODE in $conf}"
    : "${AP_APPROVE_DEPLOY_VAR:?missing AP_APPROVE_DEPLOY_VAR in $conf}"
    : "${AP_APPROVE_ROLLBACK_VAR:?missing AP_APPROVE_ROLLBACK_VAR in $conf}"
    : "${AP_BACKUP_PREFIX:?missing AP_BACKUP_PREFIX in $conf}"

    if [[ "$AP_NAME" != "$host" ]]; then
        echo "AP_NAME '$AP_NAME' does not match conf name '$host'" >&2
        return 2
    fi
    case "${AP_RUNTIME_MODE}" in
        native) ;;
        *)
            echo "AP_RUNTIME_MODE must be native in $conf" >&2
            return 2
            ;;
    esac
    AP_GOAMD64="${AP_GOAMD64:-v1}"
    case "${AP_GOAMD64}" in
        v1|v2|v3|v4) ;;
        *)
            echo "AP_GOAMD64 must be v1, v2, v3 or v4 in $conf" >&2
            return 2
            ;;
    esac
    local slots_file="$repo_root/scripts/deploy/lib/collector-slots.tsv" slot
    # shellcheck source=scripts/deploy/lib/collector-slots.sh
    . "$(dirname "${BASH_SOURCE[0]}")/collector-slots.sh"
    mapfile -t AP_SLOTS < <(collector_slots_where host "$AP_NAME" "$slots_file")
    if [[ ${#AP_SLOTS[@]} -eq 0 ]]; then
        echo "no collector slot for AP host $AP_NAME in $slots_file" >&2
        return 2
    fi
    for slot in "${AP_SLOTS[@]}"; do
        AP_SERVICES+=("$(collector_slot "$slot" service "$slots_file")") || return 2
        AP_PORTS+=("$(collector_slot "$slot" port "$slots_file")") || return 2
    done
    AP_SLOT_PREAMBLE="$(collector_slot_preamble "${AP_SLOTS[0]}" "$slots_file")" || return 2
    eval "$AP_SLOT_PREAMBLE"
    SSH_KEY="${SSH_KEY:-$HOME/.ssh/KR.key}"
    if [[ ! -r "$SSH_KEY" ]]; then
        echo "SSH key not readable: $SSH_KEY" >&2
        return 1
    fi

    AP_SSH_USER="ubuntu"
    AP_SSH_OPTIONS=(-F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10 -o SetEnv=LC_ALL=C -o SetEnv=LANG=C)
    if [[ -n "$AP_SSH_HOST_KEY_ALIAS" ]]; then
        AP_SSH_OPTIONS+=(-o HostKeyAlias="$AP_SSH_HOST_KEY_ALIAS")
    fi
    AP_SSH=(ssh "${AP_SSH_OPTIONS[@]}" "${AP_SSH_USER}@${AP_SSH_HOST}")
}

ap_rsync_rsh() {
    local arg

    printf 'ssh'
    for arg in "${AP_SSH_OPTIONS[@]}"; do
        printf ' %q' "$arg"
    done
}

ap_rsync_target() {
    local path="$1"

    printf '%s@%s:%s' "$AP_SSH_USER" "$AP_SSH_HOST" "$path"
}

ap_remote_bash() {
    local remote_cmd="bash -s --"
    local arg
    for arg in "$@"; do
        remote_cmd+=" $(printf '%q' "$arg")"
    done
    { printf '%s\n' "PATH=\"\$PATH:/usr/local/sbin:/usr/sbin:/sbin\""; cat; } | "${AP_SSH[@]}" "$remote_cmd"
}
