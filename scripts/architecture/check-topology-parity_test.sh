#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$root_dir/scripts/architecture/check-topology-parity.sh"

"$checker"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

make_fixture() {
  local destination="$1" file
  mkdir -p "$destination/deploy/topology"
  cp "$root_dir/deploy/topology/hosts.conf" "$destination/deploy/topology/hosts.conf"
  cp "$root_dir/deploy/topology/consumers.tsv" "$destination/deploy/topology/consumers.tsv"
  while IFS=$'\t' read -r file _; do
    mkdir -p "$destination/$(dirname "$file")"
    cp "$root_dir/$file" "$destination/$file"
  done <"$root_dir/deploy/topology/consumers.tsv"
}

expect_rejected() {
  local label="$1" fixture="$2"
  if TOPOLOGY_ROOT="$fixture" "$checker" >/dev/null 2>&1; then
    echo "[FAIL] topology checker accepted mutation: $label" >&2
    exit 1
  fi
}

for name in ap ap_comment env_comment env_prefix invalid_owner malformed_owner duplicate_owner unregistered dynamic_unregistered compose compose_comment compose_extra nginx nft hba extra_nginx extra_nft extra_hba broad_nft broad_nft_no_port nonssl_hba; do
  make_fixture "$tmpdir/$name"
done

sed -i 's/^AP_SSH_HOST=.*/AP_SSH_HOST=192.0.2.200/' "$tmpdir/ap/scripts/deploy/ap-hosts/osaka.conf"
expect_rejected "AP consumer" "$tmpdir/ap"

osaka_value="$(sed -n 's/^osaka=//p' "$tmpdir/ap_comment/deploy/topology/hosts.conf")"
sed -i 's/^AP_SSH_HOST=.*/AP_SSH_HOST=192.0.2.200/' "$tmpdir/ap_comment/scripts/deploy/ap-hosts/osaka.conf"
printf '# AP_SSH_HOST=%s\n' "$osaka_value" >>"$tmpdir/ap_comment/scripts/deploy/ap-hosts/osaka.conf"
expect_rejected "comment-masked AP consumer" "$tmpdir/ap_comment"

workstation_value="$(sed -n 's/^workstation=//p' "$tmpdir/env_comment/deploy/topology/hosts.conf")"
sed -i 's#^CLIPROXY_BASE_URL=.*#CLIPROXY_BASE_URL=http://192.0.2.200:8787/v1#' \
  "$tmpdir/env_comment/deploy/compose/.env.osaka.example"
printf '# prior CLIPROXY_BASE_URL=http://%s:8787/v1\n' "$workstation_value" \
  >>"$tmpdir/env_comment/deploy/compose/.env.osaka.example"
expect_rejected "comment-masked env consumer" "$tmpdir/env_comment"

workstation_value="$(sed -n 's/^workstation=//p' "$tmpdir/env_prefix/deploy/topology/hosts.conf")"
sed -i "s/$workstation_value/$workstation_value"'0/g' \
  "$tmpdir/env_prefix/deploy/compose/.env.osaka.example"
expect_rejected "owner-prefix env consumer" "$tmpdir/env_prefix"

sed -i 's/^central=.*/central=999.999.999.999/' "$tmpdir/invalid_owner/deploy/topology/hosts.conf"
expect_rejected "invalid owner octet" "$tmpdir/invalid_owner"

printf 'central =192.0.2.2\n' >>"$tmpdir/malformed_owner/deploy/topology/hosts.conf"
expect_rejected "malformed owner assignment" "$tmpdir/malformed_owner"

central_value="$(sed -n 's/^central=//p' "$tmpdir/duplicate_owner/deploy/topology/hosts.conf")"
sed -i "s/^seoul=.*/seoul=$central_value/" "$tmpdir/duplicate_owner/deploy/topology/hosts.conf"
expect_rejected "duplicate owner roles" "$tmpdir/duplicate_owner"

printf 'peer: 100.100.1.99\n' >"$tmpdir/unregistered/deploy/compose/unregistered.yml"
expect_rejected "unregistered literal consumer" "$tmpdir/unregistered"

old_osaka="$(sed -n 's/^osaka=//p' "$tmpdir/dynamic_unregistered/deploy/topology/hosts.conf")"
new_osaka=192.0.2.200
sed -i "s/^osaka=.*/osaka=$new_osaka/" "$tmpdir/dynamic_unregistered/deploy/topology/hosts.conf"
while IFS=$'\t' read -r file roles; do
  case ",$roles," in
    *,osaka,*) sed -i "s/$old_osaka/$new_osaka/g" "$tmpdir/dynamic_unregistered/$file" ;;
  esac
done <"$tmpdir/dynamic_unregistered/deploy/topology/consumers.tsv"
printf 'services:\n  unregistered:\n    ports:\n      - "%s:30096:30096"\n' "$new_osaka" \
  >"$tmpdir/dynamic_unregistered/deploy/compose/docker-compose.unregistered.yml"
expect_rejected "owner-derived unregistered Compose consumer" "$tmpdir/dynamic_unregistered"

sed -i 's/100\.100\.1\.6/192.0.2.200/g' "$tmpdir/compose/deploy/compose/docker-compose.osaka.yml"
expect_rejected "stale Compose consumer" "$tmpdir/compose"

osaka_value="$(sed -n 's/^osaka=//p' "$tmpdir/compose_comment/deploy/topology/hosts.conf")"
sed -i "s/\"$osaka_value:30096:30096\"/\"192.0.2.200:30096:30096\"/" \
  "$tmpdir/compose_comment/deploy/compose/docker-compose.osaka.yml"
printf '      # - "%s:30096:30096"\n' "$osaka_value" \
  >>"$tmpdir/compose_comment/deploy/compose/docker-compose.osaka.yml"
expect_rejected "comment-masked Compose consumer" "$tmpdir/compose_comment"

printf '      - "100.100.1.99:30097:30097"\n' \
  >>"$tmpdir/compose_extra/deploy/compose/docker-compose.osaka.yml"
expect_rejected "extra unowned Compose consumer" "$tmpdir/compose_extra"

sed -i 's/100\.100\.1\.5/192.0.2.200/g' "$tmpdir/nginx/deploy/nginx/admin-dashboard-ingress.conf.template"
expect_rejected "stale Nginx consumer" "$tmpdir/nginx"

sed -i 's/100\.100\.1\.5/192.0.2.200/g' "$tmpdir/nft/scripts/systemd/admin-dashboard-ingress.nft"
expect_rejected "stale nftables consumer" "$tmpdir/nft"

sed -i '0,/100\.100\.1\.2/s//192.0.2.200/' "$tmpdir/hba/deploy/compose/postgres/pg_hba.conf"
expect_rejected "stale PostgreSQL HBA consumer" "$tmpdir/hba"

printf '        allow 192.0.2.20;\n' >>"$tmpdir/extra_nginx/deploy/nginx/admin-dashboard-ingress.conf.template"
expect_rejected "extra Nginx allow" "$tmpdir/extra_nginx"

printf 'ip saddr 192.0.2.20 accept\n' >>"$tmpdir/extra_nft/scripts/systemd/admin-dashboard-ingress.nft"
expect_rejected "extra nftables source" "$tmpdir/extra_nft"

printf 'hostssl all all 192.0.2.20/32 scram-sha-256\n' >>"$tmpdir/extra_hba/deploy/compose/postgres/pg_hba.conf"
expect_rejected "extra PostgreSQL HBA source" "$tmpdir/extra_hba"

sed -i '/tcp dport { 30191, 30192 } reject/i\		iifname "tailscale0" tcp dport { 30191, 30192 } accept' \
  "$tmpdir/broad_nft/scripts/systemd/admin-dashboard-ingress.nft"
expect_rejected "source-free nftables accept" "$tmpdir/broad_nft"

sed -i '/tcp dport { 30191, 30192 } reject/i\		iifname "tailscale0" accept' \
  "$tmpdir/broad_nft_no_port/scripts/systemd/admin-dashboard-ingress.nft"
expect_rejected "source-free nftables accept without target port" "$tmpdir/broad_nft_no_port"

printf 'host all all 0.0.0.0/0 trust\n' >>"$tmpdir/nonssl_hba/deploy/compose/postgres/pg_hba.conf"
expect_rejected "non-loopback non-SSL HBA source" "$tmpdir/nonssl_hba"

echo "[PASS] topology owner, registration, and consumer mutations are rejected"
