#!/usr/bin/env bash

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
        def dnat($published; $internal):
            [{"match":{"op":"in","left":{"ct":{"key":"status"}},"right":"dnat"}},
             {"match":{"op":"==","left":{"ct":{"key":"proto-dst","dir":"original"}},"right":$published}},
             {"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":$internal}}];
        def sources:
            [{"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"tailscale0"}},
             {"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"@postgres_sources"}}];
        def preserve:
            [{"mangle":{"key":{"meta":{"key":"mark"}},"value":{"&":[{"meta":{"key":"mark"}},4278255615]}}}];
        [.nftables[] | .set // empty | select(.name == "postgres_sources")] as $sources |
        ($sources | length == 1) and
        ($sources[0].elem | sort == ["100.100.1.3", "100.100.1.5", "100.100.1.6", "100.100.1.8"]) and
        ([.nftables[] | .set // empty | select(.name == "bot_postgres_sources")] as $bot |
            ($bot | length == 1) and ($bot[0].elem == ["100.100.1.3"])) and
        any(.nftables[]; .chain.hook == "forward" and .chain.name == "forward" and .chain.prio == -20) and
        [.nftables[] | .rule // empty | select(.chain == "forward")] as $forward |
        ($forward | length == 8) as $retiring_pooler |
        (($forward | length == 6) or ($retiring_pooler and
            ($forward[4].expr == dnat(6433; 6432) + sources + [{"accept":null}]) and
            ($forward[5].expr == dnat(6433; 6432) + [{"reject":{"type":"tcp reset"}}]))) and
        ($forward[2].expr == dnat(5433; 5432) + sources + [{"accept":null}]) and
        ($forward[3].expr == dnat(5433; 5432) + [{"reject":{"type":"tcp reset"}}]) and
        any(.nftables[]; .chain.hook == "forward" and .chain.name == "forward_preserve_postgres_source" and .chain.prio == 10) and
        ([.nftables[] | .rule // empty | select(.chain == "forward_preserve_postgres_source") | .expr] ==
            [dnat(5433; 5432) + sources + preserve] + (if $retiring_pooler then [dnat(6433; 6432) + sources + preserve] else [] end))
    ' <<<"${rules}" >/dev/null; then
        echo '[ERROR] Active firewall is missing the approved DB source set or DNAT forward boundary' >&2
        return 1
    fi
}
