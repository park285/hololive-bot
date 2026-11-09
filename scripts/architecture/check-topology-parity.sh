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
LC_ALL=C cut -f1 "$manifest" | LC_ALL=C sort -u >"$tmpdir/registered"
(
  cd "$root_dir"
  shopt -s nullglob
  candidates=(
    scripts/deploy/ap-hosts/*.conf
    deploy/quadlet/*/*.container
    deploy/postgres/pg_hba.conf
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
# Seoul collector b는 iris-seoul Podman Quadlet이다(2026-11-06). metrics 게시 주소가 seoul tailnet 주소인지 본다.
assert_count "seoul collector b metrics bind" 1 "PublishPort=$seoul:30096:30096/tcp" "$root_dir/deploy/podman/iris-seoul/hololive-youtube-collector-b.container"

# 30192 shortlink 출처는 공개 ingress 역할(osaka2)이다. 30006/udp admin API 출처는 Console이 남는 seoul만 둔다.
public_ingress_sources=("$osaka2")
for source in "${public_ingress_sources[@]}"; do
  assert_count "central ingress public gateway source" 1 "allow $source;" \
    "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template"
done
assert_count "central ingress loopback" 1 "allow 127.0.0.1;" "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template"
assert_count "central ingress bind owner" 1 "allow @BIND_IP@;" "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template"
[[ "$(grep -Ec '^[[:space:]]*allow[[:space:]]+' "$root_dir/deploy/nginx/admin-dashboard-ingress.conf.template")" -eq \
  $((2 + ${#public_ingress_sources[@]})) ]] || fail "central ingress allow directive set"
public_ingress_nft="$(printf 'iifname "tailscale0" ip saddr %s tcp dport 30192 accept\n' "${public_ingress_sources[@]}")"
# 전체 문법과 순서를 고정해 source/target/DNAT 조건의 삭제나 넓은 accept를 거절한다.
# osaka2는 2026-11-04 collector를 내리고 공개 ingress만 맡아 DB 출처에서 빠졌다.
postgres_sources="$(printf '%s\n' "$central" "$workstation" "$seoul" "$osaka" | LC_ALL=C sort | paste -sd, - | sed 's/,/, /g')"
cat >"$tmpdir/expected-nft-grammar" <<EOF
table inet admin_dashboard_ingress {
set postgres_sources {
type ipv4_addr
elements = { $postgres_sources }
}
chain input {
type filter hook input priority -20; policy accept;
iifname "lo" tcp dport 30192 accept
$public_ingress_nft
tcp dport { 30191, 30192 } reject with tcp reset
iifname "lo" udp dport 30006 accept
iifname "tailscale0" ip saddr $seoul udp dport 30006 accept
udp dport 30006 reject
iifname "lo" tcp dport 5433 accept
iifname "tailscale0" ip saddr @postgres_sources tcp dport 5433 accept
tcp dport 5433 reject with tcp reset
}
chain forward {
type filter hook forward priority -20; policy accept;
ct status dnat ct original proto-dst 30006 udp dport 30006 iifname "tailscale0" ip saddr $seoul accept
ct status dnat ct original proto-dst 30006 udp dport 30006 reject
ct status dnat ct original proto-dst 5433 tcp dport 5432 iifname "tailscale0" ip saddr @postgres_sources accept
ct status dnat ct original proto-dst 5433 tcp dport 5432 reject with tcp reset
}
}
EOF
awk 'NF && $1 !~ /^#/ { gsub(/^[[:space:]]+|[[:space:]]+$/, ""); gsub(/[[:space:]]+/, " "); print }' \
  "$root_dir/scripts/systemd/admin-dashboard-ingress.nft" >"$tmpdir/actual-nft-grammar"
cmp -s "$tmpdir/expected-nft-grammar" "$tmpdir/actual-nft-grammar" || fail "central firewall full grammar and ordering"
assert_count "public shortlink upstream" 1 "server $central:30192;" "$root_dir/deploy/nginx/holoshi-public-shortlink.conf"

echo "[PASS] non-secret topology owner and explicit consumers are exact"
