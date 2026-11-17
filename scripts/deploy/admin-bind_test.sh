#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ "$EUID" -eq 0 ]]; then
    echo '이 fixture는 운영 경로를 쓰지 않도록 비root로만 실행한다.' >&2
    exit 1
fi
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
timeout 20 bash -c "$(declare -f run_prestart invoke_prestart); test_root='$test_root' root='$root' run_prestart"
echo 'prestart waits for an IPv6 tailnet admin bind on the IPv6 address family'

# preview도 실제 prestart 진입점을 호출하며 운영 명령은 호출 즉시 실패하게 격리한다.
printf 'WORKER_SECRET=synthetic\n' > "$test_root/secrets/alarm-worker.env"
printf 'COLLECTOR_SECRET=synthetic\n' > "$test_root/secrets/youtube-collector.env"
cat >> "$test_root/compose.env" <<'ENV'
HOLOLIVE_ALARM_WORKER_PASSWORD=alarm-secret
HOLOLIVE_COLLECTOR_C_PASSWORD=collector-secret
HOLODEX_API_KEY=synthetic-holodex
HOLOLIVE_METRICS_PORT_BIND_IP=100.100.1.8
ENV
units=(hololive-api hololive-alarm-worker hololive-youtube-collector-c)
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

# stale runtime 파일은 그대로 두고 현재 Service 환경으로만 새 파일을 만든다.
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

# 값은 compose.env를 source하지 않고 systemd가 제공한 현재 process 환경을 쓴다.
mkdir -m 0700 "$fixture/service-input"
(set -a; . "$test_root/compose.env"; set +a
 HOLOLIVE_DB_PASSWORD=service-override
 unset HOLODEX_API_KEY
 invoke_prestart --render-env hololive-api "$fixture/service-input") > "$fixture/render.log" 2>&1
grep -qx 'POSTGRES_PASSWORD=service-override' "$fixture/service-input/hololive-api.env"
grep -qx 'HOLODEX_API_KEY=' "$fixture/service-input/hololive-api.env"
[[ ! -s "$fixture/render.log" ]]

# 필수 값·여러 줄 비밀은 파일 공개 전에 거절하고 임시 파일도 남기지 않는다.
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
    hololive-youtube-collector-c:HOLOLIVE_COLLECTOR_C_PASSWORD \
    hololive-youtube-collector-c:LIVE_LOGS_PATH; do
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
        HOLODEX_API_KEY=$'injected-secret\nSECOND=value'
        invoke_prestart --render-env "$unit" "$directory"
    )
    assert_rejected_render "$directory" reject_multiline_input
done

# compose 및 서비스별 env 문법 검증도 정상 경로의 소유 검증기를 그대로 거친다.
cp "$test_root/compose.env" "$fixture/valid-compose.env"
for invalid_line in 'INVALID' 'BAD KEY=value' 'LIVE_LOGS_PATH=duplicate' 'SPACE= value'; do
    cp "$fixture/valid-compose.env" "$test_root/compose.env"
    printf '%s\n' "$invalid_line" >> "$test_root/compose.env"
    directory="$(mktemp -d "$fixture/invalid-compose.XXXXXX")"
    assert_rejected_render "$directory" invoke_prestart --render-env hololive-api "$directory"
    grep -Fq 'compose env file is invalid' "$fixture/render.log"
done
cp "$fixture/valid-compose.env" "$test_root/compose.env"
for spec in hololive-api:bot.env hololive-alarm-worker:alarm-worker.env hololive-youtube-collector-c:youtube-collector.env; do
    unit="${spec%%:*}" secret_file="${spec#*:}"
    cp "$test_root/secrets/$secret_file" "$fixture/$secret_file"
    printf 'INVALID\n' > "$test_root/secrets/$secret_file"
    directory="$fixture/invalid-container-$unit"
    mkdir -m 0700 "$directory"
    assert_rejected_render "$directory" run_prestart --render-env "$unit" "$directory"
    grep -Fq 'container env file is invalid' "$fixture/render.log"
    cp "$fixture/$secret_file" "$test_root/secrets/$secret_file"
done

# retained API 버전은 SemVer보다 넓은 기존 drop-in 허용 계약을 유지한다.
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
for unit in hololive-alarm-worker hololive-youtube-collector-c; do
    directory="$fixture/non-api-version-$unit"
    mkdir -m 0700 "$directory"
    assert_rejected_render "$directory" run_prestart --render-env "$unit" "$directory" --app-version 13.2.0
done
for unit in holo-postgres admin-dashboard-ingress collector-enabled ../hololive-api; do
    directory="$(mktemp -d "$fixture/unsupported.XXXXXX")"
    assert_rejected_render "$directory" run_prestart --render-env "$unit" "$directory"
done

# 사설 디렉터리 권한과 기존 출력의 비덮어쓰기 계약을 확인한다.
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
