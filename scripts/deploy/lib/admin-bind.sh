#!/usr/bin/env bash

# Tailscale가 보고한 canonical literal만 실제 host publish에 사용할 수 있다.
# DNS/IPv6 파서를 따로 만들지 않고 설치된 Tailscale의 주소 판정을 사용한다.
admin_bind_assert_host() {
    local bind_ip="$1"
    case "${bind_ip}" in
        100.*|fd7a:115c:a1e0:*) ;;
        *) echo '[ERROR] Admin bind must be this host canonical Tailscale address' >&2; return 1 ;;
    esac
    if ! tailscale ip --assert "${bind_ip}" >/dev/null 2>&1; then
        echo '[ERROR] Admin bind must match this host verified Tailscale identity' >&2
        return 1
    fi
}

# PostgreSQL publish 주소는 topology 정본의 primary이자 이 호스트의 Tailscale 주소여야 한다.
admin_postgres_bind_assert() {
    local root="$1" bind_ip="$2" primary="" name address
    while IFS='=' read -r name address; do
        if [[ "$name" == central ]]; then primary="$address"; fi
    done < "$root/deploy/topology/hosts.conf"
    if [[ -z "$primary" || "$bind_ip" != "$primary" ]] || ! tailscale ip --assert "$bind_ip" >/dev/null 2>&1; then
        echo '[ERROR] PostgreSQL bind must match the topology primary and this host Tailscale identity' >&2
        return 1
    fi
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
        ($sources[0].elem | sort == ["100.100.1.3", "100.100.1.5", "100.100.1.6", "100.100.1.8"]) and
        any(.nftables[]; .chain.hook == "forward" and .chain.name == "forward" and .chain.prio == -20) and
        ([.nftables[] | .rule // empty | select(.chain == "forward")] | length == 4)
    ' <<<"${rules}" >/dev/null; then
        echo '[ERROR] Active firewall is missing the approved DB source set or DNAT forward boundary' >&2
        return 1
    fi
}
