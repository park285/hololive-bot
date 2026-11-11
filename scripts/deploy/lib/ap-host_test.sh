#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fixture_root="$tmp/repo"
mkdir -p "$fixture_root/scripts/deploy/ap-hosts" "$tmp/bin"
touch "$fixture_root/KR.key"
cat > "$fixture_root/scripts/deploy/ap-hosts/osaka.conf" <<'EOF'
AP_NAME=osaka
AP_SSH_HOST=unreachable.invalid
AP_SSH_HOST_KEY_ALIAS=ap-osaka
AP_RUNTIME_MODE=native
AP_SERVICES=(youtube-collector-a)
AP_CONTAINERS=(hololive-youtube-collector-a)
AP_PORTS=(30005)
AP_APPROVE_DEPLOY_VAR=I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY
AP_APPROVE_ROLLBACK_VAR=I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK
AP_BACKUP_PREFIX=osaka-active-active
EOF

# shellcheck source=scripts/deploy/lib/ap-host.sh
. "$ROOT_DIR/scripts/deploy/lib/ap-host.sh"
SSH_KEY="$fixture_root/KR.key" ap_host_load "$fixture_root" osaka

expected_options=(-F /dev/null -i "$fixture_root/KR.key" -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10 -o SetEnv=LC_ALL=C -o SetEnv=LANG=C -o HostKeyAlias=ap-osaka)
[[ "${AP_SSH_USER}" == ubuntu ]]
[[ "${AP_SSH[*]}" == "ssh ${expected_options[*]} ubuntu@unreachable.invalid" ]]

[[ "${AP_GOAMD64}" == v1 ]]
printf 'AP_GOAMD64=v3\n' >> "$fixture_root/scripts/deploy/ap-hosts/osaka.conf"
SSH_KEY="$fixture_root/KR.key" ap_host_load "$fixture_root" osaka
[[ "${AP_GOAMD64}" == v3 ]]
sed -i 's/^AP_GOAMD64=v3$/AP_GOAMD64=v9/' "$fixture_root/scripts/deploy/ap-hosts/osaka.conf"
if SSH_KEY="$fixture_root/KR.key" ap_host_load "$fixture_root" osaka 2>/dev/null; then
  echo "[FAIL] unsupported AP_GOAMD64 must be rejected" >&2
  exit 1
fi
sed -i '/^AP_GOAMD64=/d' "$fixture_root/scripts/deploy/ap-hosts/osaka.conf"
SSH_KEY="$fixture_root/KR.key" ap_host_load "$fixture_root" osaka

# 실행 방식은 conf가 명시해야 하며 Compose AP는 허용하지 않는다.
expect_load_rejected() {
  local name="$1" message="$2"
  shift 2
  sed "$@" "$fixture_root/scripts/deploy/ap-hosts/osaka.conf" > "$fixture_root/scripts/deploy/ap-hosts/$name.conf"
  sed -i "s/^AP_NAME=osaka$/AP_NAME=$name/" "$fixture_root/scripts/deploy/ap-hosts/$name.conf"
  if (SSH_KEY="$fixture_root/KR.key" ap_host_load "$fixture_root" "$name") 2>"$tmp/$name.err"; then
    echo "[FAIL] $name conf must be rejected" >&2
    exit 1
  fi
  grep -Fq -- "$message" "$tmp/$name.err"
}
expect_load_rejected missing-mode "missing AP_RUNTIME_MODE" '/^AP_RUNTIME_MODE=/d'
expect_load_rejected compose-mode "AP_RUNTIME_MODE must be native" 's/^AP_RUNTIME_MODE=native$/AP_RUNTIME_MODE=compose/'
if (SSH_KEY="$fixture_root/KR.key" ap_host_load "$fixture_root" nonexistent-host) 2>"$tmp/unknown.err"; then
  echo "[FAIL] unknown AP host must be rejected" >&2
  exit 1
fi
grep -Fq "Unknown AP host: nonexistent-host" "$tmp/unknown.err"

rsync_rsh="$(ap_rsync_rsh)"
read -r -a parsed_rsync_rsh <<<"$rsync_rsh"
[[ "${parsed_rsync_rsh[*]}" == "ssh ${expected_options[*]}" ]]
[[ "$(ap_rsync_target './payload/')" == 'ubuntu@unreachable.invalid:./payload/' ]]


cat > "$tmp/bin/ssh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${AP_HOST_TEST_ARGV:?}"
[[ " $* " == *' -o BatchMode=yes '* ]]
[[ " $* " == *' -o ConnectTimeout=10 '* ]]
exit 255
EOF
chmod +x "$tmp/bin/ssh"
AP_SSH[0]="$tmp/bin/ssh"
if AP_HOST_TEST_ARGV="$tmp/ssh.argv" ap_remote_bash </dev/null; then
  echo "[FAIL] unreachable AP host must fail closed" >&2
  exit 1
fi
grep -Fq -- '-o BatchMode=yes' "$tmp/ssh.argv"
grep -Fq -- '-o ConnectTimeout=10' "$tmp/ssh.argv"

echo "[PASS] AP SSH and rsync share one bounded transport contract"

for conf in "$ROOT_DIR"/scripts/deploy/ap-hosts/*.conf; do
  SSH_KEY="$fixture_root/KR.key" ap_host_load "$ROOT_DIR" "$(basename "$conf" .conf)"
done
echo "[PASS] repository AP host confs pass the loader checks"
