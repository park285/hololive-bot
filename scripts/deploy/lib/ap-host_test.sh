#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fixture_root="$tmp/repo"
mkdir -p "$fixture_root/scripts/deploy/ap-hosts" "$fixture_root/deploy/compose" "$tmp/bin"
touch "$fixture_root/KR.key" "$fixture_root/deploy/compose/docker-compose.osaka.yml"
cat > "$fixture_root/scripts/deploy/ap-hosts/osaka.conf" <<'EOF'
AP_NAME=osaka
AP_SSH_HOST=unreachable.invalid
AP_SSH_HOST_KEY_ALIAS=ap-osaka
AP_COMPOSE_FILE=deploy/compose/docker-compose.osaka.yml
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

rsync_rsh="$(ap_rsync_rsh)"
read -r -a parsed_rsync_rsh <<<"$rsync_rsh"
[[ "${parsed_rsync_rsh[*]}" == "ssh ${expected_options[*]}" ]]
[[ "$(ap_rsync_target './payload/')" == 'ubuntu@unreachable.invalid:./payload/' ]]

for deploy_script in "$ROOT_DIR/scripts/deploy/ap-deploy.sh" "$ROOT_DIR/scripts/deploy/ap-host-native-deploy.sh"; do
  grep -Fq 'RSYNC_RSH="$(ap_rsync_rsh)"' "$deploy_script"
  grep -Fq '$(ap_rsync_target ' "$deploy_script"
  if grep -Eq 'RSYNC_RSH="ssh|ubuntu@\$AP_SSH_HOST' "$deploy_script"; then
    echo "[FAIL] AP deploy bypasses the canonical SSH/rsync transport owner: $deploy_script" >&2
    exit 1
  fi
done

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
