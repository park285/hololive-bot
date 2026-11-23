#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
slot_table="$root/scripts/deploy/lib/collector-slots.tsv"
# shellcheck source=scripts/deploy/lib/collector-slots.sh
. "$root/scripts/deploy/lib/collector-slots.sh"
central_slot="$(collector_slots_where host central "$slot_table")"
central_collector="$(collector_slot "$central_slot" collector_unit "$slot_table")"
central_collector="${central_collector%.service}"
central_password_key="HOLOLIVE_COLLECTOR_${central_slot^^}_PASSWORD"
if [[ "$EUID" -eq 0 ]]; then
    echo '이 fixture는 운영 경로를 쓰지 않도록 비root로만 실행한다.' >&2
    exit 1
fi
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
cp "$root/scripts/deploy/testdata/ingress-firewall.json" "$fixture/valid.json"
cp "$fixture/valid.json" "$fixture/firewall.json"
admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft"
for filter in '.nftables[0].set.elem = []' '.nftables[0].set.elem += ["0.0.0.0/0"]' 'del(.nftables[1])' 'del(.nftables[2])' \
    'del(.nftables[-1])' '.nftables[-1].set.elem += ["100.100.1.5"]' \
    'del(.nftables[4])' '.nftables[4].rule.expr[1].match.right = 6433' \
    'del(.nftables[4].rule.expr[4])' 'del(.nftables[5])' \
    '.nftables[8].chain.prio = -10' 'del(.nftables[9])' \
    '.nftables[9].rule.expr[-1].mangle.value = 0' \
    'del(.nftables[9].rule.expr[4])'; do
    jq "$filter" "$fixture/valid.json" > "$fixture/firewall.json"
    if admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft" > "$fixture/rejected.log" 2>&1; then
        echo 'incomplete firewall accepted' >&2
        exit 1
    fi
done
cp "$root/scripts/deploy/testdata/ingress-firewall-retiring-pooler.json" "$fixture/retiring.json"
cp "$fixture/retiring.json" "$fixture/firewall.json"
admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft"
for filter in 'del(.nftables[] | select(.rule.chain == "forward" and .rule.expr[1].match.right == 6433 and .rule.expr[-1].reject != null))' \
    'del(.nftables[] | select(.rule.chain == "forward_preserve_postgres_source" and .rule.expr[1].match.right == 6433))' \
    '(.nftables[] | select(.rule.chain == "forward" and .rule.expr[1].match.right == 6433 and (.rule.expr[-1] | has("accept"))) | .rule.expr) |= .[0:3] + [{"accept":null}]'; do
    jq "$filter" "$fixture/retiring.json" > "$fixture/firewall.json"
    if admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft" > "$fixture/rejected.log" 2>&1; then
        echo 'incomplete retiring pooler firewall accepted' >&2
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

test_root="$fixture/prestart"
mkdir -p "$test_root/bin" "$test_root/secrets" "$test_root/deploy/hololive/hololive-api" \
    "$test_root/deploy/scripts/systemd" "$test_root/deploy/deploy/topology"
mkdir -p "$test_root/deploy/scripts/ci" "$test_root/secrets/certs"
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
exit 2
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
invoke_prestart() {
    PATH="$test_root/bin:$PATH" HOLOLIVE_QUADLET_TEST_ROOT="$test_root" \
        bash "${root}/scripts/deploy/quadlet-prestart.sh" "$@"
}
run_prestart() {
    # shellcheck source=/dev/null
    (set -a; . "$test_root/compose.env"; set +a
     if [[ "$#" -eq 0 ]]; then set -- hololive-api; fi
     invoke_prestart "$@")
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
printf 'FIXTURE=collector\n' > "$test_root/secrets/collector.env"
printf 'HOLOLIVE_ALARM_WORKER_PASSWORD=alarm-secret\n%s=collector-secret\n' "$central_password_key" >> "$test_root/compose.env"
for service in hololive-api hololive-alarm-worker "$central_collector"; do
    rm -f "$test_root/run/quadlet-env/$service.env"
    run_prestart "$service"
    [[ -s "$test_root/run/quadlet-env/$service.env" ]]
done
echo 'all app prestarts validate the static connection budget before writing runtime secrets'

unsupported_origins=(
    units/container.d/60-pool.conf
    units/hololive-.container.d/60-pool.conf
    units/hololive-collector-.container.d/60-pool.conf
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
    for service in hololive-api hololive-alarm-worker "$central_collector"; do
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
mkdir -p "$test_root/units/hololive-api.container.d"
printf '[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n' \
    > "$test_root/units/hololive-api.container.d/60-pool.conf"
printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n' >> "$test_root/compose.env"
run_prestart
[[ -s "$test_root/run/quadlet-env/hololive-api.env" ]]
rm "$test_root/units/hololive-api.container.d/60-pool.conf"
grep -v '^ADMIN_API_POSTGRES_POOL_MAX_CONNS=' "$test_root/compose.env" > "$test_root/restored.env"
mv "$test_root/restored.env" "$test_root/compose.env"
for service in hololive-alarm-worker "$central_collector"; do
    run_prestart "$service"
done
echo 'prestart rejects unknown effective override origins before writing secrets; exact drop-ins retain compose precedence'

neutral_origins=(
    units/container.d/71-security.conf
    units/hololive-.container.d/72-security.conf
    etc/containers/systemd/hololive-api.container.d/73-security.conf
    usr/share/containers/systemd/nested/hololive-alarm-worker.container.d/74-security.conf
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

cp "$test_root/compose.env" "$test_root/within-budget.env"
printf 'BOT_POSTGRES_POOL_MAX_CONNS=40\n' >> "$test_root/compose.env"
rm "$test_root/run/quadlet-env/hololive-api.env"
if run_prestart > "$fixture/rejected.log" 2>&1; then
    echo 'prestart accepted an exhausted connection budget' >&2
    exit 1
fi
[[ ! -e "$test_root/run/quadlet-env/hololive-api.env" ]]
mv "$test_root/within-budget.env" "$test_root/compose.env"
run_prestart
echo 'prestart retains the static direct TLS connection budget'

sed -i 's/^HOLOLIVE_ADMIN_API_PORT_BIND_IP=.*/HOLOLIVE_ADMIN_API_PORT_BIND_IP=fd7a:115c:a1e0::8/' "$test_root/compose.env"
timeout 20 bash -c "$(declare -f run_prestart invoke_prestart); test_root='$test_root' root='$root' run_prestart"
echo 'prestart waits for an IPv6 tailnet admin bind on the IPv6 address family'

quadlet_bin=/usr/libexec/podman/quadlet
if [[ -x "$quadlet_bin" ]]; then
(
    cp -a "$test_root" "$fixture/generated-prestart"
    test_root="$fixture/generated-prestart"
    cp "${root}/deploy/quadlet/hololive-bot/"*.network "${root}/deploy/quadlet/hololive-bot/"*.volume "$test_root/units/"
    generated_dir="$test_root/run/systemd/generator"
    mkdir -p "$generated_dir"
    QUADLET_UNIT_DIRS="$test_root/units" "$quadlet_bin" "$generated_dir" \
        > "$fixture/generator.log" 2>&1
    run_prestart
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
    rm "$generated_dir/hololive-api.service.d/60-pool.conf"
    run_prestart
    echo 'actual generated units work; generated-service drop-ins fail before secrets publication'
)
fi

(
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
run_offline_check() (
    unset HOLOLIVE_QUADLET_TEST_ROOT
    export PATH="$fixture/offline-bin:$PATH" QUADLET_UNIT_DIRS="$foreign_roots" SYSTEMD_UNIT_PATH="$foreign_roots"
    postgres_capacity_assert_service "$root" "$fixture/offline.env" "$offline_units" hololive-api \
        "$offline_units/hololive-api.container"
)
run_offline_check
[[ ! -e "$fixture/offline-manager-called" ]]
(
    unset HOLOLIVE_QUADLET_TEST_ROOT
    units="$fixture/capped-units"
    cp -R "$offline_units" "$units"
    service="$central_collector"
    postgres_capacity_assert_service "$root" "$fixture/offline.env" "$units" "$service" "$units/$service.container"
    mkdir "$units/$service.container.d"
    printf '[Container]\nEnvironment=POSTGRES_POOL_MAX_CONNS=8\n' > "$units/$service.container.d/60-pool.conf"
    if postgres_capacity_assert_service "$root" "$fixture/offline.env" "$units" "$service" "$units/$service.container" > "$fixture/rejected.log" 2>&1; then
        echo 'capacity check accepted an installed pool cap above the role limit' >&2
        exit 1
    fi
    grep -Fq 'aggregate pools exceed role limit: hololive_collector_c' "$fixture/rejected.log"
    echo 'pre-sync capacity check validates effective writer budgets from installed units'
)
mkdir -p "$offline_units/hololive-.container.d"
cp "$foreign_roots/hololive-.container.d/60-pool.conf" "$offline_units/hololive-.container.d/"
if run_offline_check > "$fixture/rejected.log" 2>&1; then
    echo 'offline capacity check ignored an inherited override inside its explicit source tree' >&2
    exit 1
fi
grep -q 'unsupported Quadlet/systemd override origin:' "$fixture/rejected.log"
[[ ! -e "$fixture/offline-manager-called" ]]
echo 'offline capacity check excludes host origins but rejects unsupported origins inside its own tree'
)

printf 'WORKER_SECRET=synthetic\n' > "$test_root/secrets/alarm-worker.env"
printf 'COLLECTOR_SECRET=synthetic\n' > "$test_root/secrets/collector.env"
cat >> "$test_root/compose.env" <<'ENV'
HOLODEX_API_KEY=synthetic-holodex
HOLOLIVE_METRICS_PORT_BIND_IP=100.100.1.8
ENV
units=(hololive-api hololive-alarm-worker "$central_collector")
for unit in "${units[@]}"; do
    run_prestart "$unit"
    mkdir -m 0700 "$fixture/normal-$unit"
    cp "$test_root/run/quadlet-env/$unit.env" "$fixture/normal-$unit/"
done
mv "$test_root/bin" "$test_root/online-bin"
mkdir "$test_root/bin"
for command in ip tailscale nft systemctl podman docker sleep install; do
    cat > "$test_root/bin/$command" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "${0##*/}" >> "${HOLOLIVE_QUADLET_TEST_ROOT}/unexpected-operations"
exit 93
SH
    chmod +x "$test_root/bin/$command"
done

assert_private_output() {
    local directory="$1" unit="$2"
    [[ "$(stat -c '%a' "$directory")" == 700 ]]
    [[ "$(stat -c '%a' "$directory/$unit.env")" == 600 ]]
    cmp "$fixture/normal-$unit/$unit.env" "$directory/$unit.env"
    (shopt -s dotglob nullglob; files=("$directory/"*); [[ "${#files[@]}" -eq 1 ]])
    [[ ! -s "$fixture/render.log" && ! -e "$test_root/unexpected-operations" ]]
}
assert_rejected_render() {
    local directory="$1"
    shift
    if "$@" > "$fixture/render.log" 2>&1; then
        echo 'render-env accepted invalid input' >&2
        exit 1
    fi
    (shopt -s dotglob nullglob; files=("$directory/"*); [[ "${#files[@]}" -eq 0 ]])
    [[ ! -e "$test_root/unexpected-operations" ]]
    if grep -Eq 'runtime-secret|alarm-secret|collector-secret|synthetic-holodex|injected-secret' "$fixture/render.log"; then
        echo 'render-env exposed a synthetic secret in diagnostics' >&2
        exit 1
    fi
}

for unit in "${units[@]}"; do
    printf 'POSTGRES_PASSWORD=stale-secret\n' > "$test_root/run/quadlet-env/$unit.env"
    mkdir -m 0700 "$fixture/stale-$unit"
    run_prestart --render-env "$unit" "$fixture/stale-$unit" > "$fixture/render.log" 2>&1
    assert_private_output "$fixture/stale-$unit" "$unit"
    [[ "$(cat "$test_root/run/quadlet-env/$unit.env")" == POSTGRES_PASSWORD=stale-secret ]]
done
mv "$test_root/run" "$test_root/previous-run"
for unit in "${units[@]}"; do
    mkdir -m 0700 "$fixture/missing-$unit"
    run_prestart --render-env "$unit" "$fixture/missing-$unit" > "$fixture/render.log" 2>&1
    assert_private_output "$fixture/missing-$unit" "$unit"
    [[ ! -e "$test_root/run" ]]
done

mkdir -m 0700 "$fixture/service-input"
(set -a; . "$test_root/compose.env"; set +a
 export HOLOLIVE_DB_PASSWORD=service-override
 unset HOLODEX_API_KEY
 invoke_prestart --render-env hololive-api "$fixture/service-input") > "$fixture/render.log" 2>&1
grep -qx 'POSTGRES_PASSWORD=service-override' "$fixture/service-input/hololive-api.env"
grep -qx 'HOLODEX_API_KEY=' "$fixture/service-input/hololive-api.env"
[[ ! -s "$fixture/render.log" ]]

for spec in \
    hololive-api:HOLOLIVE_DB_PASSWORD \
    hololive-api:LIVE_LOGS_PATH \
    hololive-api:VALKEY_SENTINEL_ADDRS \
    hololive-api:HOLOLIVE_API_PORT_BIND_IP \
    hololive-api:HOLOLIVE_ADMIN_API_PORT_BIND_IP \
    hololive-api:HOLOLIVE_H3_SERVER_NAME \
    hololive-alarm-worker:HOLOLIVE_ALARM_WORKER_PASSWORD \
    hololive-alarm-worker:LIVE_LOGS_PATH \
    hololive-alarm-worker:VALKEY_SENTINEL_ADDRS \
    "$central_collector:$central_password_key" \
    "$central_collector:LIVE_LOGS_PATH"; do
    unit="${spec%%:*}" key="${spec#*:}"
    directory="$fixture/required-$unit-$key"
    mkdir -m 0700 "$directory"
    reject_missing_input() (
        set -a; . "$test_root/compose.env"; set +a
        unset "$key"
        invoke_prestart --render-env "$unit" "$directory"
    )
    assert_rejected_render "$directory" reject_missing_input
    grep -Fq "$key" "$fixture/render.log"
done
for unit in "${units[@]}"; do
    directory="$fixture/multiline-$unit"
    mkdir -m 0700 "$directory"
    reject_multiline_input() (
        set -a; . "$test_root/compose.env"; set +a
        export HOLODEX_API_KEY=$'injected-secret\nSECOND=value'
        invoke_prestart --render-env "$unit" "$directory"
    )
    assert_rejected_render "$directory" reject_multiline_input
done

cp "$test_root/compose.env" "$fixture/valid-compose.env"
for invalid_line in 'INVALID' 'BAD KEY=value' 'LIVE_LOGS_PATH=duplicate' 'SPACE= value' 'SPACE=value ' $'COMMAND=$(id)' $'COMMAND=`id`' $'CONTROL=a\x01b'; do
    cp "$fixture/valid-compose.env" "$test_root/compose.env"
    printf '%s\n' "$invalid_line" >> "$test_root/compose.env"
    directory="$(mktemp -d "$fixture/invalid-compose.XXXXXX")"
    assert_rejected_render "$directory" invoke_prestart --render-env hololive-api "$directory"
    grep -Fq 'compose env file is invalid' "$fixture/render.log"
done
cp "$fixture/valid-compose.env" "$test_root/compose.env"
for spec in hololive-api:bot.env hololive-alarm-worker:alarm-worker.env "$central_collector:collector.env"; do
    unit="${spec%%:*}" secret_file="${spec#*:}"
    cp "$test_root/secrets/$secret_file" "$fixture/$secret_file"
    printf 'INVALID\n' > "$test_root/secrets/$secret_file"
    directory="$fixture/invalid-container-$unit"
    mkdir -m 0700 "$directory"
    assert_rejected_render "$directory" run_prestart --render-env "$unit" "$directory"
    grep -Fq 'container env file is invalid' "$fixture/render.log"
    cp "$fixture/$secret_file" "$test_root/secrets/$secret_file"
done

mv "$test_root/deploy/hololive/hololive-api/VERSION" "$fixture/VERSION"
directory="$fixture/version-missing"
mkdir -m 0700 "$directory"
assert_rejected_render "$directory" run_prestart --render-env hololive-api "$directory"
grep -Fq 'VERSION is missing' "$fixture/render.log"
directory="$fixture/version-retained"
mkdir -m 0700 "$directory"
run_prestart --render-env hololive-api "$directory" --app-version 13.2.0-rc.1+retained > "$fixture/render.log" 2>&1
grep -qx 'APP_VERSION=13.2.0-rc.1+retained' "$directory/hololive-api.env"
[[ ! -s "$fixture/render.log" ]]
mv "$fixture/VERSION" "$test_root/deploy/hololive/hololive-api/VERSION"
for version in '' '13.2.0%unsafe' '13.2.0 unsafe' $'13.2.0\nOTHER=value'; do
    directory="$(mktemp -d "$fixture/invalid-version.XXXXXX")"
    assert_rejected_render "$directory" run_prestart --render-env hololive-api "$directory" --app-version "$version"
done
for unit in hololive-alarm-worker "$central_collector"; do
    directory="$fixture/non-api-version-$unit"
    mkdir -m 0700 "$directory"
    assert_rejected_render "$directory" run_prestart --render-env "$unit" "$directory" --app-version 13.2.0
done
for unit in holo-postgres admin-dashboard-ingress collector-enabled ../hololive-api; do
    directory="$(mktemp -d "$fixture/unsupported.XXXXXX")"
    assert_rejected_render "$directory" run_prestart --render-env "$unit" "$directory"
done

directory="$fixture/public-output"
mkdir -m 0755 "$directory"
assert_rejected_render "$directory" run_prestart --render-env hololive-api "$directory"
[[ "$(stat -c '%a' "$directory")" == 755 ]]
directory="$fixture/absent-output"
assert_rejected_render "$directory" run_prestart --render-env hololive-api "$directory"
[[ ! -e "$directory" ]]
mkdir -m 0700 "$fixture/link-target"
ln -s "$fixture/link-target" "$fixture/link-output"
assert_rejected_render "$fixture/link-target" run_prestart --render-env hololive-api "$fixture/link-output"
directory="$fixture/overwrite"
mkdir -m 0700 "$directory"
printf 'do-not-overwrite\n' > "$directory/hololive-api.env"
if run_prestart --render-env hololive-api "$directory" > "$fixture/render.log" 2>&1; then
    echo 'render-env overwrote an existing output' >&2
    exit 1
fi
[[ "$(cat "$directory/hololive-api.env")" == do-not-overwrite ]]
directory="$fixture/dangling-output"
mkdir -m 0700 "$directory"
ln -s "$fixture/nonexistent" "$directory/hololive-api.env"
if run_prestart --render-env hololive-api "$directory" > "$fixture/render.log" 2>&1; then
    echo 'render-env replaced a dangling output symlink' >&2
    exit 1
fi
[[ -L "$directory/hololive-api.env" && ! -e "$fixture/nonexistent" ]]
[[ ! -e "$test_root/run" && ! -e "$test_root/unexpected-operations" ]]
mkdir -m 0700 "$test_root/run" "$test_root/run/preview"
assert_rejected_render "$test_root/run/preview" run_prestart --render-env hololive-api "$test_root/run/preview"
rmdir "$test_root/run/preview" "$test_root/run"
echo 'private env rendering preserves current inputs, validation, permissions and offline boundaries'
