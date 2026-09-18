#!/usr/bin/env bash

# Tailscale가 보고한 canonical literal만 실제 host publish에 사용할 수 있다.
# DNS/IPv6 파서를 따로 만들지 않고 설치된 Tailscale의 주소 판정을 사용한다.
admin_network_preflight() {
    local root="$1" env_file="$2" file
    local firewall_required=false
    shift 2
    admin_bind_assert_host "$env_file" || return 1
    admin_source_assert_allowlist "$env_file" || return 1
    compose_env_key_exists_in_file "$env_file" HOLOLIVE_ADMIN_API_PORT_BIND_IP && firewall_required=true
    for file in "$@"; do
        [[ "${file##*/}" != docker-compose.live-compat.yml ]] || firewall_required=true
    done
    if [[ "$firewall_required" == true ]]; then
        admin_network_assert_firewall "$root/scripts/systemd/admin-dashboard-ingress.nft" \
            /etc/nftables.d/admin-dashboard-ingress.nft
    fi
}

admin_bind_assert_host() {
    local env_file="$1"
    local bind_ip
    compose_env_key_exists_in_file "${env_file}" HOLOLIVE_ADMIN_API_PORT_BIND_IP || return 0
    bind_ip="$(compose_env_read_value_from_file "${env_file}" HOLOLIVE_ADMIN_API_PORT_BIND_IP)" || return 1
    case "${bind_ip}" in
        100.*|fd7a:115c:a1e0:*) ;;
        *) echo '[ERROR] Admin bind must be this host canonical Tailscale address' >&2; return 1 ;;
    esac
    if ! tailscale ip --assert "${bind_ip}" >/dev/null 2>&1; then
        echo '[ERROR] Admin bind must match this host verified Tailscale identity' >&2
        return 1
    fi
}

admin_source_assert_allowlist() {
    local env_file="$1"
    local value source
    local -a sources=()
    value="$(compose_env_read_value_from_file "${env_file}" ADMIN_ALLOWED_IPS)" || return 1
    # Compose의 기존 unset/empty 기본값과 일치한다.
    value="${value:-127.0.0.1/32,::1/128,100.100.1.5/32}"
    case "${value}" in
        ,*|*,|*,,*) echo '[ERROR] Admin source list contains an empty entry' >&2; return 1 ;;
    esac
    IFS=',' read -r -a sources <<<"${value}"
    for source in "${sources[@]}"; do
        case "${source}" in
            127.0.0.1/32|::1/128|100.100.1.5/32) ;;
            *) echo '[ERROR] Admin sources must be loopback or the approved Iris Admin host' >&2; return 1 ;;
        esac
    done
}

# unit이 예전 ruleset으로 active인 경우도 source set/forward chain 확인에서 거절한다.
admin_network_assert_firewall() {
    local expected_file="$1"
    local installed_file="$2"
    local rules
    if ! cmp -s "${expected_file}" "${installed_file}" ||
        ! systemctl is-active --quiet admin-dashboard-ingress-firewall.service; then
        echo '[ERROR] Reviewed ingress firewall must be installed and active before cutover' >&2
        return 1
    fi
    if ! rules="$(nft -j list table inet admin_dashboard_ingress)"; then
        echo '[ERROR] Cannot inspect the active ingress firewall before cutover' >&2
        return 1
    fi
    if ! jq -e '
        [.nftables[] | .set // empty | select(.name == "postgres_sources")] as $sources |
        ($sources | length == 1) and
        ($sources[0].elem | sort == ["100.100.1.2", "100.100.1.3", "100.100.1.5", "100.100.1.6", "100.100.1.8"]) and
        any(.nftables[]; .chain.hook == "forward" and .chain.name == "forward" and .chain.prio == -20) and
        ([.nftables[] | .rule // empty | select(.chain == "forward")] | length == 4)
    ' <<<"${rules}" >/dev/null; then
        echo '[ERROR] Active firewall is missing the approved DB source set or Docker forward boundary' >&2
        return 1
    fi
}
