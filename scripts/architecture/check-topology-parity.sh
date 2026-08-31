#!/usr/bin/env bash
set -euo pipefail

root_dir="${TOPOLOGY_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
owner="$root_dir/deploy/topology/hosts.conf"
manifest="$root_dir/deploy/topology/consumers.tsv"

fail() {
  echo "[FAIL] topology consumer parity mismatch: $1" >&2
  exit 1
}

topology_value() {
  local role="$1" value count octet
  local -a octets
  count="$(grep -Ec "^${role}=" "$owner" 2>/dev/null || true)"
  [[ "$count" -eq 1 ]] || fail "owner role cardinality"
  value="$(awk -F= -v role="$role" '$1 == role { print $2 }' "$owner")"
  [[ "$value" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || fail "owner address format"
  IFS=. read -r -a octets <<<"$value"
  for octet in "${octets[@]}"; do
    (( 10#$octet <= 255 )) || fail "owner address range"
  done
  printf '%s' "$value"
}

assert_count() {
  local label="$1" expected_count="$2" needle="$3" file="$4" count
  count="$(awk -v needle="$needle" '
    /^[[:space:]]*#/ { next }
    {
      line = $0
      sub(/[[:space:]]+#.*/, "", line)
      while ((position = index(line, needle)) > 0) {
        count++
        line = substr(line, position + length(needle))
      }
    }
    END { print count + 0 }
  ' "$file" 2>/dev/null || true)"
  [[ "$count" -eq "$expected_count" ]] || fail "$label"
}

assert_assignment() {
  local label="$1" expected_key="$2" expected_value="$3" file="$4"
  awk -F= -v key="$expected_key" -v value="$expected_value" '
    /^[[:space:]]*#/ { next }
    $1 == key {
      count++
      if ($2 == value) matches++
    }
    END { exit !(count == 1 && matches == 1) }
  ' "$file" || fail "$label"
}

central="$(topology_value central)"
workstation="$(topology_value workstation)"
seoul="$(topology_value seoul)"
osaka="$(topology_value osaka)"
osaka2="$(topology_value osaka2)"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
printf '%s\n' central workstation seoul osaka osaka2 | LC_ALL=C sort >"$tmpdir/expected-owner-keys"
awk -F= '/^[a-z][a-z0-9_]*=/ { print $1 }' "$owner" | LC_ALL=C sort >"$tmpdir/actual-owner-keys"
cmp -s "$tmpdir/expected-owner-keys" "$tmpdir/actual-owner-keys" || fail "owner key set"
printf '%s\n' "central=$central" "workstation=$workstation" "seoul=$seoul" "osaka=$osaka" "osaka2=$osaka2" | \
  LC_ALL=C sort >"$tmpdir/expected-owner-rows"
awk 'NF && $1 !~ /^#/ { print }' "$owner" | LC_ALL=C sort >"$tmpdir/actual-owner-rows"
cmp -s "$tmpdir/expected-owner-rows" "$tmpdir/actual-owner-rows" || fail "owner row grammar"
printf '%s\n' "$central" "$workstation" "$seoul" "$osaka" "$osaka2" | LC_ALL=C sort -u >"$tmpdir/distinct"
[[ "$(wc -l <"$tmpdir/distinct")" -eq 5 ]] || fail "owner roles must have distinct addresses"
cp "$tmpdir/distinct" "$tmpdir/owner-addresses"

LC_ALL=C sort -u "$manifest" >"$tmpdir/sorted-manifest"
cmp -s "$manifest" "$tmpdir/sorted-manifest" || fail "consumer manifest must be sorted and duplicate-free"
LC_ALL=C cut -f1 "$manifest" | sort -u >"$tmpdir/registered"
(
  cd "$root_dir"
  shopt -s nullglob
  candidates=(
    scripts/deploy/ap-hosts/*.conf
    deploy/compose/*.yml
    deploy/compose/*.example
    deploy/compose/.*.example
    deploy/compose/postgres/pg_hba.conf
    deploy/nginx/*.conf
    deploy/nginx/*.template
    scripts/systemd/*.nft
  )
  for candidate in "${candidates[@]}"; do
    if awk '
      NR == FNR { owner[$0] = 1; next }
      function governed(value, octets, count, idx) {
        if (owner[value]) return 1
        if (value == "100.100.100.100") return 0
        count = split(value, octets, ".")
        if (count != 4) return 0
        for (idx = 1; idx <= count; idx++) {
          if (octets[idx] !~ /^[0-9]+$/ || octets[idx] + 0 > 255) return 0
        }
        return octets[1] + 0 == 100 && octets[2] + 0 >= 64 && octets[2] + 0 <= 127
      }
      /^[[:space:]]*#/ { next }
      {
        line = $0
        sub(/[[:space:]]+#.*/, "", line)
        gsub(/[^0-9.]/, " ", line)
        field_count = split(line, fields, /[[:space:]]+/)
        for (i = 1; i <= field_count; i++) {
          if (governed(fields[i])) found = 1
        }
      }
      END { exit !found }
    ' "$tmpdir/owner-addresses" "$candidate"; then
      printf '%s\n' "$candidate"
    fi
  done
) | LC_ALL=C sort -u >"$tmpdir/discovered"
cmp -s "$tmpdir/registered" "$tmpdir/discovered" || fail "literal consumer registration"

while IFS=$'\t' read -r path roles; do
  [[ -n "$path" && -n "$roles" && "$path" != /* && "$path" != *..* ]] || fail "consumer manifest row"
  IFS=, read -r -a allowed_roles <<<"$roles"
  : >"$tmpdir/allowed"
  for role in "${allowed_roles[@]}"; do
    case "$role" in
      central|workstation|seoul|osaka|osaka2) printf '%s\n' "${!role}" >>"$tmpdir/allowed" ;;
      *) fail "consumer manifest role" ;;
    esac
  done
  LC_ALL=C sort -u -o "$tmpdir/allowed" "$tmpdir/allowed"
  awk '
    NR == FNR { owner[$0] = 1; next }
    function governed(value, octets, count, idx) {
      if (owner[value]) return 1
      if (value == "100.100.100.100") return 0
      count = split(value, octets, ".")
      if (count != 4) return 0
      for (idx = 1; idx <= count; idx++) {
        if (octets[idx] !~ /^[0-9]+$/ || octets[idx] + 0 > 255) return 0
      }
      return octets[1] + 0 == 100 && octets[2] + 0 >= 64 && octets[2] + 0 <= 127
    }
    /^[[:space:]]*#/ { next }
    {
      line = $0
      sub(/[[:space:]]+#.*/, "", line)
      gsub(/[^0-9.]/, " ", line)
      field_count = split(line, fields, /[[:space:]]+/)
      for (i = 1; i <= field_count; i++) {
        if (governed(fields[i])) print fields[i]
      }
    }
  ' "$tmpdir/owner-addresses" "$root_dir/$path" | LC_ALL=C sort -u >"$tmpdir/observed"
  cmp -s "$tmpdir/allowed" "$tmpdir/observed" || fail "registered consumer role assignment"
done <"$manifest"

assert_assignment "osaka AP host" AP_SSH_HOST "$osaka" "$root_dir/scripts/deploy/ap-hosts/osaka.conf"
assert_assignment "osaka AP host-key alias" AP_SSH_HOST_KEY_ALIAS "$osaka" "$root_dir/scripts/deploy/ap-hosts/osaka.conf"
assert_assignment "osaka2 AP host" AP_SSH_HOST "$osaka2" "$root_dir/scripts/deploy/ap-hosts/osaka2.conf"
assert_assignment "osaka2 AP host-key alias" AP_SSH_HOST_KEY_ALIAS "$osaka2" "$root_dir/scripts/deploy/ap-hosts/osaka2.conf"
assert_assignment "seoul AP host" AP_SSH_HOST "$seoul" "$root_dir/scripts/deploy/ap-hosts/seoul.conf"
assert_assignment "seoul AP host-key alias" AP_SSH_HOST_KEY_ALIAS "$seoul" "$root_dir/scripts/deploy/ap-hosts/seoul.conf"

assert_count "osaka Compose bind" 1 "\"$osaka:30096:30096\"" "$root_dir/deploy/compose/docker-compose.osaka.yml"
assert_count "osaka2 Compose bind" 1 "\"$osaka2:30096:30096\"" "$root_dir/deploy/compose/docker-compose.osaka2.yml"
assert_count "seoul Compose bind" 1 "\"$seoul:30096:30096\"" "$root_dir/deploy/compose/docker-compose.seoul.yml"
assert_count "prod Iris host aliases" 2 ":$seoul\"" "$root_dir/deploy/compose/docker-compose.prod.yml"
assert_count "standby primary default" 1 "HOLOLIVE_PRIMARY_HOST:-$central" "$root_dir/deploy/compose/docker-compose.standby.yml"
assert_count "live-compat PostgreSQL bind" 1 "POSTGRES_PORT_BIND_IP:-$workstation" "$root_dir/deploy/compose/docker-compose.live-compat.yml"
assert_count "live-compat Valkey bind" 1 "VALKEY_PORT_BIND_IP:-$workstation" "$root_dir/deploy/compose/docker-compose.live-compat.yml"
assert_count "live-compat Iris allowlist" 2 "IRIS_BASE_URL_ALLOWED_HOSTS:-$seoul" "$root_dir/deploy/compose/docker-compose.live-compat.yml"
assert_count "live-compat H3 identity" 1 "HOLOLIVE_H3_SERVER_NAME:-$workstation" "$root_dir/deploy/compose/docker-compose.live-compat.yml"
assert_count "live-compat H3 bind" 2 "HOLOLIVE_API_PORT_BIND_IP:-$workstation" "$root_dir/deploy/compose/docker-compose.live-compat.yml"

assert_count "central ingress source" 2 "allow $seoul;" "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template"
assert_count "central ingress loopback" 2 "allow 127.0.0.1;" "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template"
assert_count "central ingress bind owner" 2 "allow @BIND_IP@;" "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template"
[[ "$(grep -Ec '^[[:space:]]*allow[[:space:]]+' "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template")" -eq 6 ]] ||
  fail "central ingress allow directive set"
assert_count "central firewall source" 1 "ip saddr $seoul tcp dport" "$root_dir/scripts/systemd/admin-dashboard-ingress.nft"
[[ "$(grep -Ec 'ip saddr' "$root_dir/scripts/systemd/admin-dashboard-ingress.nft")" -eq 1 ]] ||
  fail "central firewall source directive set"
printf '%s\n' \
  'iifname "lo" tcp dport { 30191, 30192 } accept' \
  "iifname \"tailscale0\" ip saddr $seoul tcp dport { 30191, 30192 } accept" \
  'tcp dport { 30191, 30192 } reject with tcp reset' | LC_ALL=C sort >"$tmpdir/expected-nft-ports"
awk '/tcp dport/ { gsub(/^[[:space:]]+|[[:space:]]+$/, ""); gsub(/[[:space:]]+/, " "); print }' \
  "$root_dir/scripts/systemd/admin-dashboard-ingress.nft" | LC_ALL=C sort >"$tmpdir/actual-nft-ports"
cmp -s "$tmpdir/expected-nft-ports" "$tmpdir/actual-nft-ports" || fail "central firewall target-port rule set"
printf '%s\n' \
  'iifname "lo" tcp dport { 30191, 30192 } accept' \
  "iifname \"tailscale0\" ip saddr $seoul tcp dport { 30191, 30192 } accept" | LC_ALL=C sort >"$tmpdir/expected-nft-accepts"
awk '/[[:space:]]accept([[:space:]]|$)/ { gsub(/^[[:space:]]+|[[:space:]]+$/, ""); gsub(/[[:space:]]+/, " "); print }' \
  "$root_dir/scripts/systemd/admin-dashboard-ingress.nft" | LC_ALL=C sort >"$tmpdir/actual-nft-accepts"
cmp -s "$tmpdir/expected-nft-accepts" "$tmpdir/actual-nft-accepts" || fail "central firewall accept verdict set"
printf '%s\n' \
  'table inet admin_dashboard_ingress {' \
  'chain input {' \
  'type filter hook input priority -20; policy accept;' \
  'iifname "lo" tcp dport { 30191, 30192 } accept' \
  "iifname \"tailscale0\" ip saddr $seoul tcp dport { 30191, 30192 } accept" \
  'tcp dport { 30191, 30192 } reject with tcp reset' \
  '}' \
  '}' >"$tmpdir/expected-nft-grammar"
awk 'NF { gsub(/^[[:space:]]+|[[:space:]]+$/, ""); gsub(/[[:space:]]+/, " "); print }' \
  "$root_dir/scripts/systemd/admin-dashboard-ingress.nft" >"$tmpdir/actual-nft-grammar"
cmp -s "$tmpdir/expected-nft-grammar" "$tmpdir/actual-nft-grammar" || fail "central firewall full grammar and ordering"
assert_count "public shortlink upstream" 1 "server $central:30192;" "$root_dir/deploy/nginx/holoshi-public-shortlink.conf"
assert_count "replication HBA source" 1 "hostssl replication     hololive_replicator  $seoul/32" "$root_dir/deploy/compose/postgres/pg_hba.conf"

printf '%s\n' "$central" "$workstation" "$seoul" "$osaka" "$osaka2" | LC_ALL=C sort -u >"$tmpdir/expected"
awk '$1 == "hostssl" && $2 == "all" && $3 == "all" && $4 ~ /^100\./ { sub(/\/32$/, "", $4); print $4 }' \
  "$root_dir/deploy/compose/postgres/pg_hba.conf" | LC_ALL=C sort -u >"$tmpdir/actual"
cmp -s "$tmpdir/expected" "$tmpdir/actual" || fail "PostgreSQL client allowlist"
printf '%s\n' '172.16.0.0/12' "$central/32" "$workstation/32" "$seoul/32" "$osaka/32" "$osaka2/32" | \
  LC_ALL=C sort -u >"$tmpdir/expected-hba"
awk '$1 == "hostssl" && $2 == "all" && $3 == "all" { print $4 }' \
  "$root_dir/deploy/compose/postgres/pg_hba.conf" | LC_ALL=C sort -u >"$tmpdir/actual-hba"
cmp -s "$tmpdir/expected-hba" "$tmpdir/actual-hba" || fail "PostgreSQL exact hostssl client directive set"
[[ "$(awk '$1 == "hostssl" && $2 == "replication" { count++ } END { print count + 0 }' "$root_dir/deploy/compose/postgres/pg_hba.conf")" -eq 1 ]] ||
  fail "PostgreSQL exact remote replication directive set"
printf '%s\n' \
  'local|all|all|trust' \
  'host|all|all|127.0.0.1/32|trust' \
  'host|all|all|::1/128|trust' \
  'local|replication|all|trust' \
  'host|replication|all|127.0.0.1/32|trust' \
  'host|replication|all|::1/128|trust' \
  "hostssl|replication|hololive_replicator|$seoul/32|scram-sha-256" \
  'hostssl|all|all|172.16.0.0/12|scram-sha-256' \
  "hostssl|all|all|$central/32|scram-sha-256" \
  "hostssl|all|all|$workstation/32|scram-sha-256" \
  "hostssl|all|all|$seoul/32|scram-sha-256" \
  "hostssl|all|all|$osaka/32|scram-sha-256" \
  "hostssl|all|all|$osaka2/32|scram-sha-256" | LC_ALL=C sort >"$tmpdir/expected-hba-rows"
awk 'NF && $1 !~ /^#/ { output=$1; for (i=2; i<=NF; i++) output=output "|" $i; print output }' \
  "$root_dir/deploy/compose/postgres/pg_hba.conf" | LC_ALL=C sort >"$tmpdir/actual-hba-rows"
cmp -s "$tmpdir/expected-hba-rows" "$tmpdir/actual-hba-rows" || fail "PostgreSQL complete HBA row set"

echo "[PASS] non-secret topology owner and explicit consumers are exact"
