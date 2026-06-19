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
    AP_COMPOSE_FILE=""
    AP_SERVICES=()
    AP_CONTAINERS=()
    AP_PORTS=()
    AP_APPROVE_DEPLOY_VAR=""
    AP_APPROVE_ROLLBACK_VAR=""
    AP_BACKUP_PREFIX=""

    # shellcheck disable=SC1090
    . "$conf"

    : "${AP_NAME:?missing AP_NAME in $conf}"
    : "${AP_SSH_HOST:?missing AP_SSH_HOST in $conf}"
    : "${AP_COMPOSE_FILE:?missing AP_COMPOSE_FILE in $conf}"
    : "${AP_APPROVE_DEPLOY_VAR:?missing AP_APPROVE_DEPLOY_VAR in $conf}"
    : "${AP_APPROVE_ROLLBACK_VAR:?missing AP_APPROVE_ROLLBACK_VAR in $conf}"
    : "${AP_BACKUP_PREFIX:?missing AP_BACKUP_PREFIX in $conf}"

    if [[ "$AP_NAME" != "$host" ]]; then
        echo "AP_NAME '$AP_NAME' does not match conf name '$host'" >&2
        return 2
    fi
    if [[ ${#AP_SERVICES[@]} -eq 0 || ${#AP_CONTAINERS[@]} -eq 0 || ${#AP_PORTS[@]} -eq 0 ]]; then
        echo "AP_SERVICES/AP_CONTAINERS/AP_PORTS must be non-empty in $conf" >&2
        return 2
    fi
    if [[ ${#AP_SERVICES[@]} -ne ${#AP_CONTAINERS[@]} || ${#AP_SERVICES[@]} -ne ${#AP_PORTS[@]} ]]; then
        echo "AP_SERVICES/AP_CONTAINERS/AP_PORTS must have matching lengths in $conf" >&2
        return 2
    fi
    if [[ ! -r "$repo_root/$AP_COMPOSE_FILE" ]]; then
        echo "AP_COMPOSE_FILE not readable: $repo_root/$AP_COMPOSE_FILE" >&2
        return 2
    fi

    SSH_KEY="${SSH_KEY:-$repo_root/KR.key}"
    if [[ ! -r "$SSH_KEY" ]]; then
        echo "SSH key not readable: $SSH_KEY" >&2
        return 1
    fi

    AP_SSH=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C)
    if [[ -n "$AP_SSH_HOST_KEY_ALIAS" ]]; then
        AP_SSH+=(-o HostKeyAlias="$AP_SSH_HOST_KEY_ALIAS")
    fi
    AP_SSH+=("ubuntu@$AP_SSH_HOST")
}

# 인자를 %q로 인용한다: ssh가 원격 argv를 공백으로 재조립·재파싱하므로, 인용 없이 bash -s -- "$@"로 넘기면 값의 셸 메타문자가 원격에서 실행된다.
ap_remote_bash() {
    local remote_cmd="bash -s --"
    local arg
    for arg in "$@"; do
        remote_cmd+=" $(printf '%q' "$arg")"
    done
    "${AP_SSH[@]}" "$remote_cmd"
}
