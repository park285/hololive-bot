#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/deploy/lib/compose-env.sh
source "${root}/scripts/deploy/lib/compose-env.sh"
# shellcheck source=scripts/deploy/lib/admin-bind.sh
source "${root}/scripts/deploy/lib/admin-bind.sh"
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
tailscale() {
    [[ "$1" == ip && "$2" == --assert ]]
    [[ "$3" == 100.100.1.8 || "$3" == fd7a:115c:a1e0::8 ]]
}
for bind_ip in 100.100.1.8 fd7a:115c:a1e0::8; do
    printf 'HOLOLIVE_ADMIN_API_PORT_BIND_IP=%s\n' "$bind_ip" > "$fixture/host.env"
    admin_bind_assert_host "$fixture/host.env"
done
for bind_ip in '' 0.0.0.0 :: 127.0.0.1 224.0.0.1 ff02::1 example.invalid 100.100.1.9 100.100.1.8.example.invalid 100.100.1.256 fd7a:115c:a1e0::9 fd7a:115c:a1e0::8%eth0; do
    printf 'HOLOLIVE_ADMIN_API_PORT_BIND_IP=%s\n' "$bind_ip" > "$fixture/host.env"
    if admin_bind_assert_host "$fixture/host.env" > "$fixture/rejected.log" 2>&1; then
        echo 'invalid admin bind accepted' >&2
        exit 1
    fi
done
printf 'HOLOLIVE_ADMIN_API_PORT_BIND_IP=100.100.1.8\n' > "$fixture/host.env"
tailscale() { return 1; }
if admin_bind_assert_host "$fixture/host.env" > "$fixture/rejected.log" 2>&1; then
    echo 'unknown host identity accepted' >&2
    exit 1
fi
echo 'admin bind host identity fixtures passed'

for sources in '127.0.0.1/32,::1/128,100.100.1.5/32' '127.0.0.1/32' ''; do
    printf 'ADMIN_ALLOWED_IPS=%s\n' "$sources" > "$fixture/host.env"
    admin_source_assert_allowlist "$fixture/host.env"
done
for sources in '172.16.0.0/12' '0.0.0.0/0' '::/0' '*' '100.100.1.9/32' '127.0.0.1/32,,100.100.1.5/32' '127.0.0.1/32,'; do
    printf 'ADMIN_ALLOWED_IPS=%s\n' "$sources" > "$fixture/host.env"
    if admin_source_assert_allowlist "$fixture/host.env" > "$fixture/rejected.log" 2>&1; then
        echo 'unapproved admin source accepted' >&2
        exit 1
    fi
done

cp "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft"
systemctl() { [[ "${inactive:-false}" == false ]]; }
nft() { cat "$fixture/firewall.json"; }
cat > "$fixture/valid.json" <<'JSON'
{"nftables":[
  {"set":{"name":"postgres_sources","elem":["100.100.1.2","100.100.1.3","100.100.1.5","100.100.1.6","100.100.1.8"]}},
  {"chain":{"hook":"forward","name":"forward","prio":-20}},
  {"rule":{"chain":"forward"}}, {"rule":{"chain":"forward"}},
  {"rule":{"chain":"forward"}}, {"rule":{"chain":"forward"}}
]}
JSON
cp "$fixture/valid.json" "$fixture/firewall.json"
admin_network_assert_firewall "${root}/scripts/systemd/admin-dashboard-ingress.nft" "$fixture/installed.nft"
for filter in '.nftables[0].set.elem = []' '.nftables[0].set.elem += ["0.0.0.0/0"]' 'del(.nftables[1])' 'del(.nftables[-1])'; do
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

# 실제 mutation 진입점이 build/up 전에 거부하는지 확인한다. Docker는 fixture뿐이다.
mkdir "$fixture/bin"
cat > "$fixture/bin/docker" <<'SH'
#!/usr/bin/env bash
if [[ "$*" == 'compose version' ]]; then exit 0; fi
if [[ "$1" == inspect ]]; then exit 1; fi
touch "${ADMIN_BIND_FIXTURE_EFFECT:?}"
exit 0
SH
chmod +x "$fixture/bin/docker"
printf '#!/usr/bin/env bash\nprintf "fixture-host\\n"\n' > "$fixture/bin/hostname"
chmod +x "$fixture/bin/hostname"
printf 'HOLOLIVE_ADMIN_API_PORT_BIND_IP=0.0.0.0\n' > "$fixture/host.env"
for entrypoint in compose redeploy build; do
    case "$entrypoint" in
        compose) command=(bash "${root}/scripts/deploy/compose.sh" up --build hololive-api) ;;
        redeploy) command=(bash "${root}/scripts/deploy/compose-redeploy-service.sh" hololive-api) ;;
        build) command=(bash "${root}/build-all.sh" --no-bump) ;;
    esac
    if PATH="$fixture/bin:$PATH" ADMIN_BIND_FIXTURE_EFFECT="$fixture/effect" COMPOSE_ENV_FILE="$fixture/host.env" \
        "${command[@]}" > "$fixture/rejected.log" 2>&1; then
        echo "$entrypoint accepted a wildcard admin bind" >&2
        exit 1
    fi
    grep -Fq 'Admin bind must be this host canonical Tailscale address' "$fixture/rejected.log" || {
        cat "$fixture/rejected.log" >&2
        exit 1
    }
    [[ ! -e "$fixture/effect" ]]
    echo "$entrypoint rejects invalid admin bind before build or start"
done
