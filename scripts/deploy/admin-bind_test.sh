#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/deploy/lib/admin-bind.sh
source "${root}/scripts/deploy/lib/admin-bind.sh"
source "${root}/scripts/deploy/lib/postgres-capacity.sh"
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
  {"rule":{"chain":"forward","expr":[
    {"match":{"op":"in","left":{"ct":{"key":"status"}},"right":"dnat"}},
    {"match":{"op":"==","left":{"ct":{"key":"proto-dst","dir":"original"}},"right":6433}},
    {"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":6432}},
    {"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"tailscale0"}},
    {"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"@postgres_sources"}},
    {"accept":null}
  ]}},
  {"rule":{"chain":"forward","expr":[
    {"match":{"op":"in","left":{"ct":{"key":"status"}},"right":"dnat"}},
    {"match":{"op":"==","left":{"ct":{"key":"proto-dst","dir":"original"}},"right":6433}},
    {"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":6432}},
    {"reject":{"type":"tcp reset"}}
  ]}},
  {"rule":{"chain":"forward"}}, {"rule":{"chain":"forward"}},
  {"chain":{"hook":"forward","name":"forward_preserve_postgres_source","prio":10}},
  {"rule":{"chain":"forward_preserve_postgres_source","expr":[
    {"match":{"op":"in","left":{"ct":{"key":"status"}},"right":"dnat"}},
    {"match":{"op":"==","left":{"ct":{"key":"proto-dst","dir":"original"}},"right":5433}},
    {"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":5432}},
    {"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"tailscale0"}},
    {"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"@postgres_sources"}},
    {"mangle":{"key":{"meta":{"key":"mark"}},"value":{"&":[{"meta":{"key":"mark"}},4278255615]}}}
  ]}},
  {"rule":{"chain":"forward_preserve_postgres_source","expr":[
    {"match":{"op":"in","left":{"ct":{"key":"status"}},"right":"dnat"}},
    {"match":{"op":"==","left":{"ct":{"key":"proto-dst","dir":"original"}},"right":6433}},
    {"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":6432}},
    {"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"tailscale0"}},
    {"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"@postgres_sources"}},
    {"mangle":{"key":{"meta":{"key":"mark"}},"value":{"&":[{"meta":{"key":"mark"}},4278255615]}}}
  ]}},
  {"set":{"name":"bot_postgres_sources","elem":["100.100.1.3"]}}
]}
JSON
cp "$fixture/valid.json" "$fixture/firewall.json"
admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft"
for filter in '.nftables[0].set.elem = []' '.nftables[0].set.elem += ["0.0.0.0/0"]' 'del(.nftables[1])' 'del(.nftables[2])' \
    'del(.nftables[-1])' '.nftables[-1].set.elem += ["100.100.1.5"]' \
    'del(.nftables[6])' '.nftables[6].rule.expr[1].match.right = 5433' \
    'del(.nftables[6].rule.expr[4])' 'del(.nftables[7])' \
    '.nftables[10].chain.prio = -10' 'del(.nftables[12])' \
    '.nftables[12].rule.expr[-1].mangle.value = 0' \
    'del(.nftables[12].rule.expr[4])'; do
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
mkdir -p "$test_root/deploy/scripts/ci" "$test_root/secrets/pgbouncer" "$test_root/secrets/certs"
cp "${root}/scripts/ci/postgres-capacity-policy.tsv" "$test_root/deploy/scripts/ci/"
mkdir -p "$test_root/units"
cp "${root}/deploy/quadlet/hololive-bot/"*.container "$test_root/units/"
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
case "$*" in
    show-environment)
        [[ ! -f "$HOLOLIVE_QUADLET_TEST_ROOT/manager-environment" ]] ||
            cat "$HOLOLIVE_QUADLET_TEST_ROOT/manager-environment"
        exit 0 ;;
    'show --property=UnitPath --value')
        printf '%s\n' "$HOLOLIVE_QUADLET_TEST_ROOT/etc/systemd/system" "$HOLOLIVE_QUADLET_TEST_ROOT/custom-systemd"
        exit 0 ;;
    show\ *.service\ --property=NeedDaemonReload\ --value)
        if [[ -e "$HOLOLIVE_QUADLET_TEST_ROOT/manager-reload-required" ]]; then echo yes; else echo no; fi
        exit 0 ;;
esac
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
cat > "$test_root/bin/podman" <<'SH'
#!/usr/bin/env bash
[[ "$*" == 'exec holo-pgbouncer sha256sum /etc/pgbouncer/pgbouncer.ini' ]] || exit 2
[[ ! -e "$HOLOLIVE_QUADLET_TEST_ROOT/query-failed" ]] || exit 125
digest="$(sha256sum "$HOLOLIVE_QUADLET_TEST_ROOT/live-pgbouncer.ini")" || exit
printf '%s  /etc/pgbouncer/pgbouncer.ini\n' "${digest%% *}"
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
postgres_capacity_write_pgbouncer_config "${root}/scripts/ci/postgres-capacity-policy.tsv" \
    "$test_root/compose.env" "$test_root/live-pgbouncer.ini" >/dev/null
run_prestart() {
    # shellcheck source=/dev/null
    (set -a; . "$test_root/compose.env"; set +a
     PATH="$test_root/bin:$PATH" HOLOLIVE_QUADLET_TEST_ROOT="$test_root" \
        bash "${root}/scripts/deploy/quadlet-prestart.sh" "${1:-hololive-api}")
}
if run_prestart > "$fixture/rejected.log" 2>&1; then
    echo 'prestart accepted a wildcard admin bind' >&2
    exit 1
fi
[[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
echo 'prestart rejects invalid admin bind before writing container secrets'

sed -i 's/^HOLOLIVE_ADMIN_API_PORT_BIND_IP=.*/HOLOLIVE_ADMIN_API_PORT_BIND_IP=100.100.1.8/' "$test_root/compose.env"
run_prestart
grep -qx 'POSTGRES_PASSWORD=runtime-secret' "$test_root/run/quadlet-env/hololive-api.env"
grep -qx 'APP_VERSION=13.3.0' "$test_root/run/quadlet-env/hololive-api.env"
[[ "$(stat -c '%a' "$test_root/run/quadlet-env/hololive-api.env")" == 600 ]]
echo 'prestart moves runtime secrets into a private container env file'

printf 'FIXTURE=worker\n' > "$test_root/secrets/alarm-worker.env"
printf 'FIXTURE=collector\n' > "$test_root/secrets/youtube-collector.env"
printf 'HOLOLIVE_ALARM_WORKER_PASSWORD=worker-secret\nHOLOLIVE_COLLECTOR_C_PASSWORD=collector-secret\n' >> "$test_root/compose.env"
for service in hololive-api hololive-alarm-worker hololive-youtube-collector-c; do
    for failure in drift missing query_failed; do
        rm -f "$test_root/run/quadlet-env/$service.env"
        case "$failure" in
            drift)
                printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' >> "$test_root/compose.env"
                mkdir -p "$test_root/run/pgbouncer"
                postgres_capacity_write_pgbouncer_config "${root}/scripts/ci/postgres-capacity-policy.tsv" \
                    "$test_root/compose.env" "$test_root/run/pgbouncer/pgbouncer.ini" >/dev/null ;;
            missing) mv "$test_root/live-pgbouncer.ini" "$test_root/stopped-pgbouncer.ini" ;;
            query_failed)
                mv "$test_root/stopped-pgbouncer.ini" "$test_root/live-pgbouncer.ini"
                touch "$test_root/query-failed" ;;
        esac
        if run_prestart "$service" > "$fixture/rejected.log" 2>&1; then
            echo "prestart accepted $failure for $service" >&2
            exit 1
        fi
        [[ ! -e "$test_root/run/quadlet-env/$service.env" ]]
        if [[ "$failure" == drift ]]; then
            grep -v '^ADMIN_API_POSTGRES_POOL_MAX_CONNS=' "$test_root/compose.env" > "$test_root/restored.env"
            mv "$test_root/restored.env" "$test_root/compose.env"
        fi
    done
    rm "$test_root/query-failed"
    run_prestart "$service"
    [[ -s "$test_root/run/quadlet-env/$service.env" ]]
done
echo 'all app prestarts require matching live mounted capacity before writing runtime secrets'

# 상속·다른 검색 루트·생성된 service의 원천을 무시하면 실제 앱은 11, pooler는 10이 된다.
unsupported_origins=(
    units/container.d/60-pool.conf
    units/hololive-.container.d/60-pool.conf
    units/hololive-youtube-.container.d/60-pool.conf
    units/holo-.container.d/60-pool.conf
    etc/containers/systemd/container.d/60-pool.conf
    run/containers/systemd/hololive-.container.d/60-pool.conf
    run/containers/systemd/users/hololive-.container.d/60-pool.conf
    usr/share/containers/systemd/nested/hololive-api.container.d/60-pool.conf
    run/containers/systemd/nested/hololive-api.container
    etc/systemd/system/hololive-api.service.d/60-pool.conf
    run/systemd/system/hololive-.service.d/60-pool.conf
    usr/lib/systemd/system/service.d/60-pool.conf
    run/systemd/generator/hololive-api.service.d/60-pool.conf
    run/systemd/generator.early/hololive-api.service
    etc/systemd/system/hololive-api.service
    custom-systemd/hololive-api.service.d/60-pool.conf
)
write_origin_override() {
    local origin="$1" section=Container
    case "$origin" in
        */systemd/system/*|*/systemd/generator*/*|custom-systemd/*) section=Service ;;
    esac
    mkdir -p "$(dirname "$test_root/$origin")"
    printf '[%s]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' "$section" > "$test_root/$origin"
}
for origin in "${unsupported_origins[@]}"; do
    write_origin_override "$origin"
    for service in hololive-api hololive-alarm-worker hololive-youtube-collector-c; do
        rm -f "$test_root/run/quadlet-env/$service.env"
        if run_prestart "$service" > "$fixture/rejected.log" 2>&1; then
            echo "prestart accepted unsupported origin: $origin ($service)" >&2
            exit 1
        fi
        grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
        [[ ! -e "$test_root/run/quadlet-env/$service.env" ]]
    done
    rm "$test_root/$origin"
done
for search_key in QUADLET_UNIT_DIRS SYSTEMD_UNIT_PATH; do
    rm -f "$test_root/run/quadlet-env/hololive-api.env"
    if (export "$search_key=$test_root/units"; run_prestart) > "$fixture/rejected.log" 2>&1; then
        echo "prestart accepted unknown search roots: $search_key" >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
    [[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
done
for manager_case in manager-environment manager-reload-required; do
    printf 'QUADLET_UNIT_DIRS=/unreviewed\n' > "$test_root/$manager_case"
    rm -f "$test_root/run/quadlet-env/hololive-api.env"
    if run_prestart > "$fixture/rejected.log" 2>&1; then
        echo "prestart accepted unknown manager state: $manager_case" >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
    [[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
    rm "$test_root/$manager_case"
done
# 지원하는 정확한 unit drop-in과 compose 우선순위는 그대로 유지한다.
mkdir -p "$test_root/units/hololive-api.container.d"
printf '[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' \
    > "$test_root/units/hololive-api.container.d/60-pool.conf"
printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n' >> "$test_root/compose.env"
run_prestart
[[ -s "$test_root/run/quadlet-env/hololive-api.env" ]]
rm "$test_root/units/hololive-api.container.d/60-pool.conf"
grep -v '^ADMIN_API_POSTGRES_POOL_MAX_CONNS=' "$test_root/compose.env" > "$test_root/restored.env"
mv "$test_root/restored.env" "$test_root/compose.env"
for service in hololive-alarm-worker hololive-youtube-collector-c; do
    run_prestart "$service"
done
echo 'prestart rejects unknown effective override origins before writing secrets; exact drop-ins retain compose precedence'

# 파일명이 아니라 제한된 내용으로 MCS-only 원천을 판정한다.
neutral_origins=(
    units/container.d/71-security.conf
    units/hololive-.container.d/72-security.conf
    etc/containers/systemd/hololive-api.container.d/73-security.conf
    usr/share/containers/systemd/nested/hololive-alarm-worker.container.d/74-security.conf
    etc/containers/systemd/holo-pgbouncer.container.d/75-security.conf
)
write_neutral_origin() {
    mkdir -p "$(dirname "$test_root/$1")"
    printf '# host MCS\n[Container]\nSecurityLabelLevel=s0:c900,c901\n' > "$test_root/$1"
}
for origin in "${neutral_origins[@]}"; do
    write_neutral_origin "$origin"
    run_prestart
    printf 'Environment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' >> "$test_root/$origin"
    rm "$test_root/run/quadlet-env/hololive-api.env"
    if run_prestart > "$fixture/rejected.log" 2>&1; then
        echo "prestart accepted capacity settings mixed into a security-only origin: $origin" >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
    [[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
    rm "$test_root/$origin"
done
origin="${neutral_origins[2]}"
for body in \
    '[Service]\nSecurityLabelLevel=s0:c900,c901\n' \
    '[Container]\nSecurityLabelLevel="s0:c900,c901"\n' \
    '[Container]\nSecurityLabelLevel=s0:c900,\\\nc901\n' \
    '[Container]\nSecurityLabelLevel=s0:c900,c901\n[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' \
    '[Container]\nSecurityLabelLevel=s0:c900,c901\nSecurityLabelLevel=s0:c902,c903\n' \
    '[Container]\nSecurityLabelLevel=s0:c900,c901\nSecurityLabelDisable=true\n'; do
    printf '%b' "$body" > "$test_root/$origin"
    if run_prestart > "$fixture/rejected.log" 2>&1; then
        echo 'prestart accepted unsupported syntax in a security-only origin' >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
    [[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
done
# 중립 파일이라도 동명 환경 파일을 가려 실제 precedence를 바꾸면 허용하지 않는다.
write_neutral_origin "$origin"
printf '[Container]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n' \
    > "$test_root/units/hololive-api.container.d/$(basename "$origin")"
if run_prestart > "$fixture/rejected.log" 2>&1; then
    echo 'prestart accepted a security-only origin masking a same-name capacity drop-in' >&2
    exit 1
fi
grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
rm "$test_root/$origin" "$test_root/units/hololive-api.container.d/$(basename "$origin")"
run_prestart
echo 'prestart preserves neutral MCS drop-ins but rejects mixed content, unsupported syntax and precedence collisions'

cp "$test_root/compose.env" "$test_root/pooled.env"
printf 'HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-postgres\nHOLOLIVE_CENTRAL_POSTGRES_PORT=5432\n' >> "$test_root/compose.env"
mv "$test_root/live-pgbouncer.ini" "$test_root/stopped-pgbouncer.ini"
for service in hololive-api hololive-alarm-worker hololive-youtube-collector-c; do
    rm "$test_root/run/quadlet-env/$service.env"
    run_prestart "$service"
    [[ -s "$test_root/run/quadlet-env/$service.env" ]]
done
printf 'BOT_POSTGRES_POOL_MAX_CONNS=40\n' >> "$test_root/compose.env"
rm "$test_root/run/quadlet-env/hololive-api.env"
if run_prestart > "$fixture/rejected.log" 2>&1; then
    echo 'direct recovery accepted an exhausted connection budget' >&2
    exit 1
fi
[[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
mv "$test_root/pooled.env" "$test_root/compose.env"
mv "$test_root/stopped-pgbouncer.ini" "$test_root/live-pgbouncer.ini"
echo 'explicit direct recovery starts without a pooler but retains the static capacity budget'

# Tailscale IPv6 admin bind는 허용 대상이므로 IPv4 주소만 찾다가 90초 뒤 실패하면 안 된다.
sed -i 's/^HOLOLIVE_ADMIN_API_PORT_BIND_IP=.*/HOLOLIVE_ADMIN_API_PORT_BIND_IP=fd7a:115c:a1e0::8/' "$test_root/compose.env"
timeout 20 bash -c "$(declare -f run_prestart); test_root='$test_root' root='$root' run_prestart"
echo 'prestart waits for an IPv6 tailnet admin bind on the IPv6 address family'

printf 'POSTGRES_PORT_BIND_IP=100.100.1.8\nADMIN_API_POSTGRES_POOL_MAX_CONNS=3\n' >> "$test_root/compose.env"
printf 'fixture-ca\n' > "$test_root/secrets/certs/postgres-ca.pem"
for tls_file in server.crt server.key backend-client.crt backend-client.key; do
    printf 'fixture-tls\n' > "$test_root/secrets/pgbouncer/$tls_file"
done
run_pooler_prestart() {
    (set -a; . "$test_root/compose.env"; set +a
     PATH="$test_root/bin:$PATH" HOLOLIVE_QUADLET_TEST_ROOT="$test_root" \
        bash "${root}/scripts/deploy/quadlet-prestart.sh" holo-pgbouncer)
}
run_pooler_prestart
pooler_config="$test_root/run/pgbouncer/pgbouncer.ini"
[[ -s "$pooler_config" && "$(stat -c '%a' "$pooler_config")" == 644 ]]
[[ ! -e "$test_root/run/quadlet-env/holo-pgbouncer.env" ]]
python3 - "$pooler_config" <<'PY'
import configparser
import shlex
import sys

config = configparser.ConfigParser(interpolation=None)
config.read(sys.argv[1])
runtime = dict(part.split("=", 1) for part in shlex.split(config["users"]["hololive_runtime"]))
assert runtime == {"pool_size": "9", "max_user_connections": "9"}
assert config.getint("pgbouncer", "max_db_connections") == 46
PY
cp "$pooler_config" "$fixture/accepted-pgbouncer.ini"
for origin in "${unsupported_origins[@]}"; do
    write_origin_override "$origin"
    if run_pooler_prestart > "$fixture/rejected.log" 2>&1; then
        echo "pooler prestart accepted unsupported origin: $origin" >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
    cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
    rm "$test_root/$origin"
done
run_pooler_prestart
cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
echo 'pooler prestart rejects unknown effective override origins without replacing the accepted config'
for origin in "${neutral_origins[@]}"; do
    write_neutral_origin "$origin"
    run_pooler_prestart
    cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
    printf 'Environment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' >> "$test_root/$origin"
    if run_pooler_prestart > "$fixture/rejected.log" 2>&1; then
        echo "pooler prestart accepted capacity settings mixed into a security-only origin: $origin" >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
    cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
    rm "$test_root/$origin"
done
# mount hash가 맞아도 pooler가 다른 config를 읽도록 명령을 바꾸면 기동 전에 거부한다.
cp "$pooler_config" "$test_root/live-pgbouncer.ini"
mkdir -p "$test_root/units/holo-pgbouncer.container.d"
for directive in 'Exec=/tmp/other.ini' 'Entrypoint=/bin/sh' 'Exec=' 'Entrypoint='; do
    printf '[Container]\n%s\n' "$directive" > "$test_root/units/holo-pgbouncer.container.d/60-command.conf"
    rm -f "$test_root/run/quadlet-env/hololive-api.env"
    if run_prestart > "$fixture/rejected.log" 2>&1; then
        echo "app prestart accepted a pooler command override: ${directive%%=*}" >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet pooler command override:' "$fixture/rejected.log"
    [[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
    if run_pooler_prestart > "$fixture/rejected.log" 2>&1; then
        echo "pooler prestart accepted a command override: ${directive%%=*}" >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet pooler command override:' "$fixture/rejected.log"
    cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
done
rm "$test_root/units/holo-pgbouncer.container.d/60-command.conf"
run_prestart
run_pooler_prestart
echo 'pooler command overrides fail before secrets or configuration publication even with a matching mounted hash'
quadlet_bin=/usr/libexec/podman/quadlet
if [[ -x "$quadlet_bin" ]]; then
    cp "${root}/deploy/quadlet/hololive-bot/"*.network "${root}/deploy/quadlet/hololive-bot/"*.volume "$test_root/units/"
    generated_dir="$test_root/run/systemd/generator"
    mkdir -p "$generated_dir"
    QUADLET_UNIT_DIRS="$test_root/units" "$quadlet_bin" "$generated_dir" \
        > "$fixture/generator.log" 2>&1
    cp "$pooler_config" "$test_root/live-pgbouncer.ini"
    run_prestart
    run_pooler_prestart
    mkdir -p "$generated_dir/hololive-api.service.d"
    printf '[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' \
        > "$generated_dir/hololive-api.service.d/60-pool.conf"
    rm "$test_root/run/quadlet-env/hololive-api.env"
    if run_prestart > "$fixture/rejected.log" 2>&1; then
        echo 'prestart accepted a drop-in overriding an actual generated service' >&2
        exit 1
    fi
    grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
    [[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
    if run_pooler_prestart > "$fixture/rejected.log" 2>&1; then
        echo 'pooler prestart accepted a drop-in overriding an actual generated app service' >&2
        exit 1
    fi
    cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
    rm "$generated_dir/hololive-api.service.d/60-pool.conf"
    run_prestart
    run_pooler_prestart
    echo 'actual generated units work; generated-service drop-ins fail before secrets or pooler config publication'
fi
for tls_file in pgbouncer/server.crt pgbouncer/server.key pgbouncer/backend-client.crt pgbouncer/backend-client.key certs/postgres-ca.pem; do
    mv "$test_root/secrets/$tls_file" "$fixture/missing-tls"
    if run_pooler_prestart > "$fixture/rejected.log" 2>&1; then
        echo 'pooler prestart accepted missing TLS material' >&2
        exit 1
    fi
    cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
    mv "$fixture/missing-tls" "$test_root/secrets/$tls_file"
done
printf '# stale firewall\n' >> "$test_root/installed.nft"
if run_pooler_prestart > "$fixture/rejected.log" 2>&1; then
    echo 'pooler prestart accepted a stale installed firewall' >&2
    exit 1
fi
cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
cp "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$test_root/installed.nft"
sed -i 's/^POSTGRES_PORT_BIND_IP=.*/POSTGRES_PORT_BIND_IP=0.0.0.0/' "$test_root/compose.env"
if run_pooler_prestart > "$fixture/rejected.log" 2>&1; then
    echo 'pooler prestart accepted a wildcard publish address' >&2
    exit 1
fi
cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
sed -i 's/^POSTGRES_PORT_BIND_IP=.*/POSTGRES_PORT_BIND_IP=100.100.1.8/' "$test_root/compose.env"
printf 'BOT_POSTGRES_POOL_MAX_CONNS=40\n' >> "$test_root/compose.env"
if run_pooler_prestart > "$fixture/rejected.log" 2>&1; then
    echo 'pooler prestart accepted an exhausted connection budget' >&2
    exit 1
fi
cmp -s "$pooler_config" "$fixture/accepted-pgbouncer.ini"
echo 'pooler prestart preserves the last config on missing TLS, invalid bind or exhausted capacity'

# 명시적 오프라인 source tree는 호스트의 설치/manager 원천과 섞지 않는다.
offline_units="$fixture/offline-units"
foreign_roots="$fixture/foreign-roots"
mkdir -p "$offline_units" "$fixture/offline-bin" \
    "$foreign_roots/hololive-.container.d" "$foreign_roots/hololive-api.service.d"
cp "${root}/deploy/quadlet/hololive-bot/"*.container "$offline_units/"
printf 'APP_ENV=production\n' > "$fixture/offline.env"
printf '[Container]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' \
    > "$foreign_roots/hololive-.container.d/60-pool.conf"
printf '[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' \
    > "$foreign_roots/hololive-api.service.d/60-pool.conf"
cat > "$fixture/offline-bin/systemctl" <<SH
#!/usr/bin/env bash
touch "$fixture/offline-manager-called"
exit 99
SH
chmod +x "$fixture/offline-bin/systemctl"
run_offline_render() (
    unset HOLOLIVE_QUADLET_TEST_ROOT
    export PATH="$fixture/offline-bin:$PATH" QUADLET_UNIT_DIRS="$foreign_roots" SYSTEMD_UNIT_PATH="$foreign_roots"
    postgres_capacity_write_quadlet_config "$root" "$fixture/offline.env" "$offline_units" "$fixture/offline.ini"
)
postgres_capacity_write_pgbouncer_config "${root}/scripts/ci/postgres-capacity-policy.tsv" \
    "$fixture/offline.env" "$fixture/offline-expected.ini" >/dev/null
run_offline_render
cmp -s "$fixture/offline.ini" "$fixture/offline-expected.ini"
[[ ! -e "$fixture/offline-manager-called" ]]
mkdir -p "$offline_units/hololive-.container.d"
cp "$foreign_roots/hololive-.container.d/60-pool.conf" "$offline_units/hololive-.container.d/"
if run_offline_render > "$fixture/rejected.log" 2>&1; then
    echo 'offline rendering ignored an inherited override inside its explicit source tree' >&2
    exit 1
fi
grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
cmp -s "$fixture/offline.ini" "$fixture/offline-expected.ini"
[[ ! -e "$fixture/offline-manager-called" ]]
echo 'offline source rendering excludes host origins but rejects unsupported origins inside its own tree'
