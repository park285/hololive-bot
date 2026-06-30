#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

tmp="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT

fixture_root="$tmp/repo"
fakebin="$tmp/bin"
mkdir -p "$fixture_root/scripts/deploy/lib" "$fixture_root/scripts/deploy/ap-hosts" "$fixture_root/deploy/compose" "$fakebin"
cp "$ROOT_DIR/scripts/deploy/lib/ap-host.sh" "$fixture_root/scripts/deploy/lib/ap-host.sh"
cat > "$fixture_root/scripts/deploy/lib/require-quic-udp-buffer.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
required="${1:?required}"
host_name="${2:?host}"
[[ "$required" == 7500000 ]]
echo "AP QUIC UDP buffers ok on ${host_name}: runtime rmem=7500000 wmem=7500000; persisted rmem=7500000 wmem=7500000"
EOF
chmod +x "$fixture_root/scripts/deploy/lib/require-quic-udp-buffer.sh"
touch "$fixture_root/KR.key" "$fixture_root/deploy/compose/docker-compose.osaka.yml"
cat > "$fixture_root/scripts/deploy/ap-hosts/osaka.conf" <<'EOF'
AP_NAME=osaka
AP_SSH_HOST=mock-host
AP_SSH_HOST_KEY_ALIAS=mock-host
AP_COMPOSE_FILE=deploy/compose/docker-compose.osaka.yml
AP_RUNTIME_MODE=native
AP_SERVICES=(youtube-producer-a)
AP_CONTAINERS=(hololive-youtube-producer-a)
AP_PORTS=(30005)
AP_APPROVE_DEPLOY_VAR=I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY
AP_APPROVE_ROLLBACK_VAR=I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK
AP_BACKUP_PREFIX=osaka-active-active
EOF

cat > "$fakebin/ssh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
remote_cmd="${@: -1}"
[[ "$remote_cmd" == bash\ -s\ --* ]] || { echo "unexpected ssh command: $remote_cmd" >&2; exit 99; }
args="${remote_cmd#bash -s --}"
eval "set -- $args"
PATH="${FAKE_REMOTE_BIN}:$PATH" bash -s -- "$@"
EOF
chmod +x "$fakebin/ssh"

cat > "$fakebin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
args=()
while (($# > 0)); do
  case "$1" in
    -n) shift ;;
    -u) shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
set -- "${args[@]}"
case "${1:-}" in
  test)
    exit 0
    ;;
  grep)
    exit 0
    ;;
  env)
    shift
    while (($# > 0)); do
      case "$1" in
        *=*) shift ;;
        *) break ;;
      esac
    done
    if [[ "${1:-}" == */bin/healthcheck ]]; then
      if [[ " $* " == *" --body "* ]]; then
        printf '{"status":"ready","version":"test"}\n'
      fi
      exit 0
    fi
    exec "$@"
    ;;
  *)
    exec "$@"
    ;;
esac
EOF
chmod +x "$fakebin/sudo"

cat > "$fakebin/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "is-active" ]]; then
  exit 0
fi
if [[ "${1:-}" == "show" ]]; then
  prop=""
  for arg in "$@"; do
    case "$arg" in
      ActiveState|SubState|NRestarts|ActiveEnterTimestamp) prop="$arg" ;;
    esac
  done
  case "$prop" in
    ActiveState) echo active ;;
    SubState) echo running ;;
    NRestarts) echo 0 ;;
    ActiveEnterTimestamp) echo "Tue 2026-06-30 08:14:12 UTC" ;;
    *) exit 1 ;;
  esac
  exit 0
fi
exit 1
EOF
chmod +x "$fakebin/systemctl"

cat > "$fakebin/journalctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat <<'LOG'
Jun 30 08:14:13 host youtube-producer-wrapper[1]: 2026-06-30T08:14:13Z INF logging/log.go:44 ingestion runtime configured active_active_enabled=true
Jun 30 08:14:13 host youtube-producer-wrapper[1]: 2026-06-30T08:14:13Z INF cache/service_connection.go:63 Cache store connected
Jun 30 08:14:13 host youtube-producer-wrapper[1]: 2026-06-30T08:14:13Z INF dbx/client.go:199 postgres_pool_connected
LOG
EOF
chmod +x "$fakebin/journalctl"

output="$(
  PATH="$fakebin:$PATH" FAKE_REMOTE_BIN="$fakebin" REPO_ROOT="$fixture_root" \
    CHANGE_STARTED_AT=2026-06-30T08:13:49Z \
    "$ROOT_DIR/scripts/deploy/ap-completion-check.sh" osaka
)"
grep -Fq 'AP QUIC UDP buffers ok on osaka' <<<"$output" || fail "native completion check runs remote UDP buffer verification"
grep -Fq '"status":"ready"' <<<"$output" || fail "native completion check verifies ready endpoint"
grep -Fq 'active-active completion check passed' <<<"$output" || fail "native completion check reports completion"
if grep -Fq 'cd ~/hololive-bot' <<<"$output"; then
  fail "native completion check must not require remote compose checkout"
fi

pass "ap-completion-check supports host-native APs"
