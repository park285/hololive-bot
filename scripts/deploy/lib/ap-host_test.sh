#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fixture_root="$tmp/repo"
mkdir -p "$fixture_root/scripts/deploy/ap-hosts" "$fixture_root/deploy/compose" "$tmp/bin"
touch "$fixture_root/KR.key" "$fixture_root/deploy/compose/docker-compose.seoul.yml"
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

# 실행 방식은 conf가 명시해야 하고, Compose overlay는 compose 호스트만 가진다.
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
expect_load_rejected native-overlay "AP_COMPOSE_FILE is only valid for AP_RUNTIME_MODE=compose" \
  's/^AP_RUNTIME_MODE=native$/&\nAP_COMPOSE_FILE=deploy\/compose\/docker-compose.seoul.yml/'
expect_load_rejected compose-no-overlay "missing AP_COMPOSE_FILE" 's/^AP_RUNTIME_MODE=native$/AP_RUNTIME_MODE=compose/'
sed -e 's/^AP_NAME=osaka$/AP_NAME=compose-host/' \
  -e 's/^AP_RUNTIME_MODE=native$/AP_RUNTIME_MODE=compose\nAP_COMPOSE_FILE=deploy\/compose\/docker-compose.seoul.yml/' \
  "$fixture_root/scripts/deploy/ap-hosts/osaka.conf" > "$fixture_root/scripts/deploy/ap-hosts/compose-host.conf"
(SSH_KEY="$fixture_root/KR.key" ap_host_load "$fixture_root" compose-host)

# Compose smoke는 native 호스트에서 원격 명령을 보내기 전에 거부한다.
mkdir -p "$fixture_root/scripts/deploy/lib" "$tmp/smoke-bin"
cp "$ROOT_DIR/scripts/deploy/lib/ap-host.sh" "$fixture_root/scripts/deploy/lib/ap-host.sh"
printf '#!/usr/bin/env bash\necho "unexpected ssh: $*" >&2\nexit 99\n' > "$tmp/smoke-bin/ssh"
chmod +x "$tmp/smoke-bin/ssh"
if PATH="$tmp/smoke-bin:$PATH" REPO_ROOT="$fixture_root" SSH_KEY="$fixture_root/KR.key" \
  bash "$ROOT_DIR/scripts/logs/ap-smoke.sh" osaka 2>"$tmp/smoke.err"; then
  echo "[FAIL] ap-smoke.sh must refuse a native AP host" >&2
  exit 1
fi
grep -Fq 'Refusing Compose AP smoke for osaka (runtime=native)' "$tmp/smoke.err"

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
