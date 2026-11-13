#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/deploy/lib/admin-bind.sh
source "${root}/scripts/deploy/lib/admin-bind.sh"
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
tailscale() {
    [[ "$1" == ip && "$2" == --assert ]]
    [[ "$3" == 100.100.1.8 || "$3" == fd7a:115c:a1e0::8 ]]
}
for bind_ip in 100.100.1.8 fd7a:115c:a1e0::8; do
    admin_bind_assert_host "$bind_ip"
done
for bind_ip in '' 0.0.0.0 :: 127.0.0.1 224.0.0.1 ff02::1 example.invalid 100.100.1.9 100.100.1.8.example.invalid 100.100.1.256 fd7a:115c:a1e0::9 fd7a:115c:a1e0::8%eth0; do
    if admin_bind_assert_host "$bind_ip" > "$fixture/rejected.log" 2>&1; then
        echo 'invalid admin bind accepted' >&2
        exit 1
    fi
done
admin_postgres_bind_assert "$root" 100.100.1.8
for bind_ip in '' 100.100.1.3 0.0.0.0; do
    if admin_postgres_bind_assert "$root" "$bind_ip" > "$fixture/rejected.log" 2>&1; then
        echo 'non-primary PostgreSQL bind accepted' >&2
        exit 1
    fi
done
tailscale() { return 1; }
if admin_bind_assert_host 100.100.1.8 > "$fixture/rejected.log" 2>&1; then
    echo 'unknown host identity accepted' >&2
    exit 1
fi
if admin_postgres_bind_assert "$root" 100.100.1.8 > "$fixture/rejected.log" 2>&1; then
    echo 'PostgreSQL bind without host identity accepted' >&2
    exit 1
fi
echo 'admin and PostgreSQL bind host identity fixtures passed'

cp "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft"
systemctl() { [[ "${inactive:-false}" == false ]]; }
nft() { cat "$fixture/firewall.json"; }
cat > "$fixture/valid.json" <<'JSON'
{"nftables":[
  {"set":{"name":"postgres_sources","elem":["100.100.1.3","100.100.1.5","100.100.1.6","100.100.1.8"]}},
  {"chain":{"hook":"forward","name":"forward","prio":-20}},
  {"rule":{"chain":"forward"}}, {"rule":{"chain":"forward"}},
  {"rule":{"chain":"forward"}}, {"rule":{"chain":"forward"}},
  {"rule":{"chain":"forward"}}, {"rule":{"chain":"forward"}},
  {"set":{"name":"bot_postgres_sources","elem":["100.100.1.3"]}}
]}
JSON
cp "$fixture/valid.json" "$fixture/firewall.json"
admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft"
for filter in '.nftables[0].set.elem = []' '.nftables[0].set.elem += ["0.0.0.0/0"]' 'del(.nftables[1])' 'del(.nftables[2])' \
    'del(.nftables[-1])' '.nftables[-1].set.elem += ["100.100.1.5"]'; do
    jq "$filter" "$fixture/valid.json" > "$fixture/firewall.json"
    if admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft" > "$fixture/rejected.log" 2>&1; then
        echo 'incomplete firewall accepted' >&2
        exit 1
    fi
done
cp "$fixture/valid.json" "$fixture/firewall.json"
inactive=true
if admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft" > "$fixture/rejected.log" 2>&1; then
    echo 'inactive firewall accepted' >&2
    exit 1
fi
inactive=false
printf '# drift\n' >> "$fixture/installed.nft"
if admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft" > "$fixture/rejected.log" 2>&1; then
    echo 'unreviewed installed firewall accepted' >&2
    exit 1
fi
echo 'admin firewall preflight fixtures passed'

# 실제 기동 진입점(Quadlet ExecStartPre)이 비밀 env를 쓰기 전에 거부하는지 확인한다.
test_root="$fixture/prestart"
mkdir -p "$test_root/bin" "$test_root/secrets" "$test_root/deploy/hololive/hololive-api" \
    "$test_root/deploy/scripts/systemd" "$test_root/deploy/deploy/topology"
cp "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$test_root/deploy/scripts/systemd/"
cp "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$test_root/installed.nft"
cp "${root}/deploy/topology/hosts.conf" "$test_root/deploy/deploy/topology/"
printf '13.3.0\n' > "$test_root/deploy/hololive/hololive-api/VERSION"
printf 'API_SECRET_KEY=x\n' > "$test_root/secrets/bot.env"
cat > "$test_root/bin/tailscale" <<'SH'
#!/usr/bin/env bash
[[ "$1 $2" == 'ip --assert' && ( "$3" == 100.100.1.8 || "$3" == fd7a:115c:a1e0::8 ) ]]
SH
cat > "$test_root/bin/systemctl" <<'SH'
#!/usr/bin/env bash
exit 0
SH
cat > "$test_root/bin/nft" <<SH
#!/usr/bin/env bash
cat '$fixture/valid.json'
SH
cat > "$test_root/bin/ip" <<'SH'
#!/usr/bin/env bash
if [[ "$1" == -6 ]]; then
    printf '5: tailscale0    inet6 fd7a:115c:a1e0::8/128 scope global\n'
else
    printf '5: tailscale0    inet 100.100.1.8/32 scope global tailscale0\n'
fi
SH
chmod +x "$test_root/bin/"*
cat > "$test_root/compose.env" <<'ENV'
HOLOLIVE_DB_PASSWORD=runtime-secret
LIVE_LOGS_PATH=/var/log/hololive
VALKEY_SENTINEL_ADDRS=100.100.1.3:26379
HOLOLIVE_API_PORT_BIND_IP=100.100.1.8
HOLOLIVE_ADMIN_API_PORT_BIND_IP=0.0.0.0
HOLOLIVE_H3_SERVER_NAME=100.100.1.8
ENV
run_prestart() {
    # shellcheck source=/dev/null
    (set -a; . "$test_root/compose.env"; set +a
     PATH="$test_root/bin:$PATH" HOLOLIVE_QUADLET_TEST_ROOT="$test_root" \
        bash "${root}/scripts/deploy/quadlet-prestart.sh" hololive-api)
}
if run_prestart > "$fixture/rejected.log" 2>&1; then
    echo 'prestart accepted a wildcard admin bind' >&2
    exit 1
fi
grep -Fq 'Admin bind must be this host canonical Tailscale address' "$fixture/rejected.log" || {
    cat "$fixture/rejected.log" >&2
    exit 1
}
[[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
echo 'prestart rejects invalid admin bind before writing container secrets'

sed -i 's/^HOLOLIVE_ADMIN_API_PORT_BIND_IP=.*/HOLOLIVE_ADMIN_API_PORT_BIND_IP=100.100.1.8/' "$test_root/compose.env"
run_prestart
grep -qx 'POSTGRES_PASSWORD=runtime-secret' "$test_root/run/quadlet-env/hololive-api.env"
grep -qx 'APP_VERSION=13.3.0' "$test_root/run/quadlet-env/hololive-api.env"
[[ "$(stat -c '%a' "$test_root/run/quadlet-env/hololive-api.env")" == 600 ]]
echo 'prestart moves runtime secrets into a private container env file'

# Tailscale IPv6 admin bind는 허용 대상이므로 IPv4 주소만 찾다가 90초 뒤 실패하면 안 된다.
sed -i 's/^HOLOLIVE_ADMIN_API_PORT_BIND_IP=.*/HOLOLIVE_ADMIN_API_PORT_BIND_IP=fd7a:115c:a1e0::8/' "$test_root/compose.env"
timeout 20 bash -c "$(declare -f run_prestart); test_root='$test_root' root='$root' run_prestart"
echo 'prestart waits for an IPv6 tailnet admin bind on the IPv6 address family'
