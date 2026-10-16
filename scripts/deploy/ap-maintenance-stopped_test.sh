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
run_preflight restarting-in-maintenance true restarting
[[ "$rc" != 0 ]] || fail 'maintenance preflight accepted a restarting collector'
run_preflight paused-in-maintenance true paused
[[ "$rc" != 0 ]] || fail 'maintenance preflight accepted a paused collector'
run_preflight stopped-without-maintenance false exited
[[ "$rc" != 0 ]] || fail 'ordinary preflight accepted a stopped collector'
run_preflight running-ordinary false running
[[ "$rc" == 0 ]] || fail "ordinary preflight refused a healthy collector: $(<"$case_dir/out")"

# ap-deploy.sh의 기준점 기록 원격 문자열을 그대로 렌더링해 실행한다. 이 단계는 rollback을 걸기 전이다.
capture_block="$(sed -n '/^rollback_available=false$/,/^echo backup_dir=/p' "$ROOT_DIR/scripts/deploy/ap-deploy.sh" | sed -n '/^remote "set -euo pipefail$/,$p')"
[[ "$capture_block" == 'remote "set -euo pipefail'* ]] || fail 'baseline capture block was not found'
capture_block="${capture_block#remote \"}"
capture_block="${capture_block%\"}"
. "$ROOT_DIR/scripts/deploy/lib/ap-prechange-config.sh"
# shellcheck disable=SC2034 # 렌더링하는 원격 문자열이 읽는 ap-deploy.sh 변수다.
render_capture() {
  local AP_COLLECTOR_MAINTENANCE_STOPPED="$1" backup_dir=backups/seoul-collector-20261016T000000Z \
    IMAGE_REF=hololive-youtube-collector:prod PO_IMAGE_REF=hololive-youtube-po-sandbox:prod \
    rollback_image_tag=hololive-youtube-collector:rollback-20261016T000000Z \
    rollback_po_image_tag=hololive-youtube-po-sandbox:rollback-20261016T000000Z \
    po_manifest_active=backups/po-sandbox-current-b.json po_image_id_active=backups/po-sandbox-current-b.image-id \
    PROD_COMPOSE_FILE=deploy/compose/docker-compose.prod.yml AP_COMPOSE_FILE=deploy/compose/docker-compose.seoul.yml
  eval "printf '%s\n' \"$capture_block\""
}
mkdir -p "$tmp/capture-bin"
cat > "$tmp/capture-bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
image_file() { printf '%s/images/%s' "$STATE" "${1//[:\/]/_}"; }
case "$1" in
  image)
    [[ "$2" == inspect ]] || exit 40
    if [[ "$3" != -f ]]; then [[ -f "$(image_file "$3")" ]]; exit; fi
    read -r id revision version < "$(image_file "$5")"
    case "$4" in
      '{{.Id}}') echo "$id" ;;
      *revision*) echo "$revision" ;;
      *version*) echo "$version" ;;
      '{{.Os}}/{{.Architecture}}') echo linux/arm64 ;;
      *) exit 41 ;;
    esac
    ;;
  inspect)
    [[ "$2" == -f ]] || exit 42
    read -r status image < "$STATE/containers/$4"
    case "$3" in
      '{{.State.Status}}') echo "$status" ;;
      '{{.Image}}') echo "$image" ;;
      *) exit 43 ;;
    esac
    ;;
  tag) cp "$(image_file "$2")" "$(image_file "$3")" ;;
  ps) ;;
  *) exit 44 ;;
esac
DOCKER
printf '#!/usr/bin/env bash\necho docker\n' > "$tmp/capture-bin/groups"
chmod +x "$tmp/capture-bin/docker" "$tmp/capture-bin/groups"
collector_revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
other_revision=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

run_capture() {
  local name="$1" maintenance="$2" collector_status="$3" issuer_revision="$4" collector_image="${5:-present}"
  local container_image="${CONTAINER_IMAGE_INDEX:-1}" issuer_version="${ISSUER_VERSION:-10.1.0}"
  case_dir="$tmp/capture-$name"
  local repo="$case_dir/home/hololive-bot"
  mkdir -p "$case_dir/images" "$case_dir/containers" "$repo/backups" "$repo/deploy/compose" "$repo/scripts/deploy"
  if [[ "$collector_image" == present ]]; then
    printf 'sha256:%064d %s 10.1.0\n' 1 "$collector_revision" > "$case_dir/images/hololive-youtube-collector_prod"
  fi
  printf 'sha256:%064d %s %s\n' 2 "$issuer_revision" "$issuer_version" > "$case_dir/images/hololive-youtube-po-sandbox_prod"
  printf '%s sha256:%064d\n' "$collector_status" "$container_image" > "$case_dir/containers/hololive-youtube-collector-b"
  printf 'running sha256:%064d\n' 2 > "$case_dir/containers/hololive-youtube-po-b"
  printf '{"source_revision": "%s"}\n' "$issuer_revision" > "$repo/backups/po-sandbox-current-b.json"
  printf 'sha256:%064d\n' 2 > "$repo/backups/po-sandbox-current-b.image-id"
  touch "$repo/deploy/compose/docker-compose.prod.yml" "$repo/deploy/compose/docker-compose.seoul.yml"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$repo/scripts/deploy/compose.sh"
  chmod +x "$repo/scripts/deploy/compose.sh"
  render_capture "$maintenance" > "$case_dir/capture.sh"
  rc=0
  (cd "$repo" && HOME="$case_dir/home" STATE="$case_dir" PATH="$tmp/capture-bin:$tmp/bin:$PATH" bash "$case_dir/capture.sh") \
    > "$case_dir/out" 2>&1 || rc="$?"
  state_file="$repo/backups/seoul-collector-20261016T000000Z/collector-prechange.state"
}

run_capture maintenance-stopped true exited "$collector_revision"
[[ "$rc" == 0 ]] || fail "maintenance baseline refused a stopped collector: $(<"$case_dir/out")"
[[ "$(<"$state_file")" == stopped ]] || fail 'maintenance baseline did not record a stopped collector'
[[ -f "$case_dir/images/hololive-youtube-collector_rollback-20261016T000000Z" ]] || fail 'maintenance baseline did not tag the stopped collector image'
run_capture maintenance-running true running "$collector_revision"
[[ "$rc" != 0 ]] || fail 'maintenance baseline accepted a running collector'
run_capture maintenance-first-deploy true exited "$collector_revision" absent
if [[ "$rc" == 0 ]] || ! grep -q 'requires the current collector image' "$case_dir/out"; then
  fail 'maintenance baseline accepted a deploy without a collector image'
fi
run_capture ordinary-stopped false exited "$collector_revision"
[[ "$rc" != 0 ]] || fail 'ordinary baseline accepted a stopped collector'
run_capture ordinary-running false running "$collector_revision"
[[ "$rc" == 0 && "$(<"$state_file")" == running ]] || fail "ordinary baseline did not record a running collector: $(<"$case_dir/out")"
run_capture mismatched-pair false running "$other_revision"
if [[ "$rc" == 0 ]] || ! grep -q 'different revisions; refusing cutover' "$case_dir/out"; then
  fail 'baseline accepted an issuer from another revision before arming rollback'
fi

CONTAINER_IMAGE_INDEX=3 run_capture maintenance-other-image true exited "$collector_revision"
[[ "$rc" != 0 ]] || fail 'maintenance baseline accepted a stopped collector that is not the current :prod image'
ISSUER_VERSION=10.0.0 run_capture mismatched-version false running "$collector_revision"
if [[ "$rc" == 0 ]] || ! grep -q 'different versions; refusing cutover' "$case_dir/out"; then
  fail 'baseline accepted an issuer from another version before arming rollback'
fi

# 실제 ap-rollback.sh를 fake SSH로 실행해 원격 완료 줄과 로컬 판별이 같은 계약을 쓰는지 확인한다.
mkdir -p "$tmp/ssh-bin"
cat > "$tmp/ssh-bin/ssh" <<'SSH'
#!/usr/bin/env bash
set -euo pipefail
command="${*: -1}"
case "$command" in
  *' check '*) echo 'AP rollback verified: fixture' ;;
  date*) echo 2026-10-16T00:00:00Z ;;
  *' apply '*)
    echo 'AP source snapshot restored'
    [[ "${FAKE_APPLY_RC:-0}" == 0 ]] || exit "$FAKE_APPLY_RC"
    # shellcheck disable=SC2034 # 원격 rollback의 실제 완료 printf가 읽는 값이다.
    backup=fixture started_at=2026-10-16T00:00:00Z old_collector_tag=fixture collector_state="$FAKE_STATE" old_po_tag=fixture
    eval "$COMPLETION_LINE"
    ;;
  *) cat > /dev/null; echo status >> "$STATE/status-calls" ;;
esac
SSH
chmod +x "$tmp/ssh-bin/ssh"
completion_line="$(grep -E "^printf 'AP rollback complete: " "$rollback_script")"
[[ -n "$completion_line" ]] || fail 'remote rollback completion line was not found'
approve_var="$(cd "$ROOT_DIR" && SSH_KEY=/dev/null bash -c '. scripts/deploy/lib/ap-host.sh; ap_host_load "$PWD" seoul >/dev/null; printf %s "$AP_APPROVE_ROLLBACK_VAR"')"
[[ -n "$approve_var" ]] || fail 'seoul rollback approval variable was not found'
# fake SSH는 PATH로만 끼워 넣으므로, 호스트 설정이 절대 경로 ssh를 쓰면 실제 호스트에 닿기 전에 멈춘다.
ssh_command="$(cd "$ROOT_DIR" && SSH_KEY=/dev/null bash -c '. scripts/deploy/lib/ap-host.sh; ap_host_load "$PWD" seoul >/dev/null; printf %s "${AP_SSH[0]}"')"
[[ "$ssh_command" == ssh ]] || fail "seoul SSH command is not resolved through PATH: $ssh_command"

run_rollback() {
  local name="$1" state="$2" apply_rc="$3"
  case_dir="$tmp/rollback-$name"
  mkdir -p "$case_dir"
  rc=0
  env "$approve_var=true" BACKUP_DIR=backups/seoul-collector-20261016T000000Z SSH_KEY=/dev/null \
    STATE="$case_dir" FAKE_STATE="$state" FAKE_APPLY_RC="$apply_rc" COMPLETION_LINE="$completion_line" PATH="$tmp/ssh-bin:$PATH" \
    bash "$ROOT_DIR/scripts/deploy/ap-rollback.sh" seoul --apply < /dev/null > "$case_dir/out" 2>&1 || rc="$?"
}

run_rollback stopped stopped 0
[[ "$rc" == 0 ]] || fail "stopped rollback failed: $(<"$case_dir/out")"
grep -q 'collector restored stopped' "$case_dir/out" || fail 'stopped rollback did not report the stopped collector'
[[ ! -e "$case_dir/status-calls" ]] || fail 'stopped rollback probed a collector that must stay stopped'
run_rollback running running 0
[[ "$rc" == 0 && -e "$case_dir/status-calls" ]] || fail "running rollback did not check the restored collector: $(<"$case_dir/out")"
run_rollback failed stopped 7
[[ "$rc" != 0 ]] || fail 'failed remote rollback was reported as success'
grep -q 'AP source snapshot restored' "$case_dir/out" || fail 'failed rollback lost the remote progress output'
! grep -q 'collector restored stopped' "$case_dir/out" || fail 'failed rollback reported a stopped restore'

echo 'AP maintenance-stopped collector checks passed'
