#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
rollback_script="$ROOT_DIR/scripts/deploy/lib/po-ap-rollback-remote.sh"
preflight_script="$ROOT_DIR/scripts/deploy/ap-collector-preflight.sh"
fail() { echo "[FAIL] $*" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"

# 실제 rollback 함수와 원격 사전 검사 본문을 실행한다. Docker·Compose·sudo만 상태 파일로 대체한다.
restore_body="$(sed -n '/^restore_previous_collector() {$/,/^}$/p' "$rollback_script")"
preflight_body="$(sed -n "/<<'REMOTE'$/,/^REMOTE$/{//!p}" "$preflight_script")"
[[ -n "$restore_body" && -n "$preflight_body" ]] || fail 'tested bodies were not found'

cat > "$tmp/bin/sudo" <<'SUDO'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == -n ]] && shift
case "$1" in
  test|openssl) exit 0 ;;
esac
exec "$@"
SUDO
cat > "$tmp/bin/compose" <<'COMPOSE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$STATE/calls"
case " $* " in
  *' up --no-start '*) [[ "${FAKE_START:-false}" == true ]] && echo running > "$STATE/status" || echo created > "$STATE/status" ;;
  *' up -d '*) echo running > "$STATE/status" ;;
  *) exit 41 ;;
esac
COMPOSE
cat > "$tmp/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  inspect)
    [[ "${2:-}" == -f ]] || exit 0
    case "$3" in
      *Labels*) cat "$STATE/revision" ;;
      *Health*) [[ "$(<"$STATE/status")" == running ]] && echo healthy || cat "$STATE/status" ;;
      '{{.State.Status}}') cat "$STATE/status" ;;
      *) exit 42 ;;
    esac
    ;;
  exec) printf 'exec %s\n' "$2" >> "$STATE/calls" ;;
  *) exit 43 ;;
esac
DOCKER
chmod +x "$tmp/bin/sudo" "$tmp/bin/compose" "$tmp/bin/docker"

run_restore() {
  local name="$1" collector_state="$2" status="$3" revision="$4"
  case_dir="$tmp/restore-$name"
  mkdir -p "$case_dir"
  printf '%s\n' "$status" > "$case_dir/status"
  printf '%s\n' "$revision" > "$case_dir/revision"
  cat > "$case_dir/run.sh" <<RUN
set -euo pipefail
compose=("$tmp/bin/compose")
collector_state='$collector_state' prod_source=deploy/compose/docker-compose.prod.yml ap_source=deploy/compose/docker-compose.seoul.yml
service=youtube-collector-b container=hololive-youtube-collector-b port=30015 old_revision=old-revision
$restore_body
restore_previous_collector
RUN
  rc=0
  STATE="$case_dir" PATH="$tmp/bin:$PATH" bash "$case_dir/run.sh" > "$case_dir/out" 2>&1 || rc="$?"
}

run_restore stopped stopped exited old-revision
[[ "$rc" == 0 ]] || fail "stopped baseline restore failed: $(<"$case_dir/out")"
[[ "$(<"$case_dir/status")" == created ]] || fail 'stopped baseline collector was started'
grep -q -- ' up --no-start --no-build --no-deps --force-recreate youtube-collector-b$' "$case_dir/calls" || fail 'stopped baseline was not recreated without start'
! grep -q -- ' up -d \|^exec ' "$case_dir/calls" || fail 'stopped baseline restore started or probed the collector'

FAKE_START=true run_restore started-anyway stopped exited old-revision
[[ "$rc" != 0 ]] || fail 'restore accepted a running collector for a stopped baseline'

run_restore stopped-wrong-image stopped exited old-revision
printf 'new-revision\n' > "$case_dir/revision"
STATE="$case_dir" PATH="$tmp/bin:$PATH" bash "$case_dir/run.sh" > "$case_dir/out" 2>&1 && fail 'restore accepted a stopped collector from another image'

run_restore running running running old-revision
[[ "$rc" == 0 ]] || fail "running baseline restore failed: $(<"$case_dir/out")"
grep -q -- ' up -d --no-build --no-deps --force-recreate youtube-collector-b$' "$case_dir/calls" || fail 'running baseline was not started'
grep -q '^exec hololive-youtube-collector-b$' "$case_dir/calls" || fail 'running baseline health was not probed'

run_preflight() {
  local name="$1" allow_stopped="$2" status="$3"
  case_dir="$tmp/preflight-$name"
  mkdir -p "$case_dir"
  printf '%s\n' "$status" > "$case_dir/status"
  printf '%s\n' old-revision > "$case_dir/revision"
  rc=0
  {
    printf 'groups() { echo docker; }\n'
    printf '%s\n' "$preflight_body"
  } | STATE="$case_dir" PATH="$tmp/bin:$PATH" AP_PREFLIGHT_ALLOW_FIRST_BOOT=false AP_PREFLIGHT_ALLOW_STOPPED="$allow_stopped" \
    AP_CONTAINERS_LIST=hololive-youtube-collector-b AP_PORTS_LIST=30015 AP_NAME=seoul bash -s > "$case_dir/out" 2>&1 || rc="$?"
}

run_preflight stopped-allowed true exited
[[ "$rc" == 0 ]] || fail "maintenance preflight refused a stopped collector: $(<"$case_dir/out")"
run_preflight created-allowed true created
[[ "$rc" == 0 ]] || fail "maintenance preflight refused a created collector: $(<"$case_dir/out")"
run_preflight running-in-maintenance true running
[[ "$rc" != 0 ]] || fail 'maintenance preflight accepted a running collector'
run_preflight stopped-without-maintenance false exited
[[ "$rc" != 0 ]] || fail 'ordinary preflight accepted a stopped collector'
run_preflight running-ordinary false running
[[ "$rc" == 0 ]] || fail "ordinary preflight refused a healthy collector: $(<"$case_dir/out")"

echo 'AP maintenance-stopped collector checks passed'
