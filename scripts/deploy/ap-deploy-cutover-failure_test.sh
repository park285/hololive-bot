#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
deploy_script="$ROOT_DIR/scripts/deploy/ap-deploy.sh"
rollback_script="$ROOT_DIR/scripts/deploy/ap-rollback.sh"
rollback_remote_script="$ROOT_DIR/scripts/deploy/lib/po-ap-rollback-remote.sh"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
remote_log="$tmp/remote.log"
stderr_log="$tmp/stderr.log"

handler_body="$(sed -n '/^restore_after_failed_deploy() {$/,/^}$/p' "$deploy_script")"
stop_body="$(sed -n '/^stop_failed_first_deploy() {$/,/^}$/p' "$deploy_script")"
[[ -n "$handler_body" && -n "$stop_body" ]] \
  || fail "ap-deploy.sh must define restore_after_failed_deploy and stop_failed_first_deploy"

fake_root="$tmp/repo"
mkdir -p "$fake_root/scripts/deploy"
rollback_log="$tmp/rollback.log"
cat > "$fake_root/scripts/deploy/ap-rollback.sh" <<'FAKE'
#!/usr/bin/env bash
printf 'BACKUP_DIR=%s APPROVE=%s args=%s\n' "$BACKUP_DIR" "${AP_TEST_APPROVE_ROLLBACK:-}" "$*" >> "$ROLLBACK_LOG"
exit "${FAKE_ROLLBACK_STATUS:-0}"
FAKE
chmod +x "$fake_root/scripts/deploy/ap-rollback.sh"

# 실패 처리 함수를 실패 상태로 실행하고 원격·rollback 호출을 기록한다. 실제 원격 접속은 하지 않는다.
# remote_status는 원격 정지 명령이 실패한 경우를 흉내 낸다.
run_failed_deploy() {
  local cutover="$1"
  local source="$2"
  local rollback="$3"
  local status="$4"
  local remote_status="${5:-0}"

  : > "$remote_log"
  : > "$stderr_log"
  : > "$rollback_log"
  # shellcheck disable=SC2034,SC2329 # eval로 불러온 함수가 아래 변수와 remote·ssh 스텁을 읽는다.
  (
    remote() {
      printf '%s\n' "$*" >> "$remote_log"
      return "$remote_status"
    }
    fake_ssh() {
      printf 'ssh %s\n' "$*" >> "$remote_log"
    }
    eval "$handler_body"
    eval "$stop_body"
    export ROLLBACK_LOG="$rollback_log"
    AP_NAME="ap-test"
    AP_APPROVE_ROLLBACK_VAR="AP_TEST_APPROVE_ROLLBACK"
    AP_SSH=(fake_ssh)
    REPO_ROOT="$fake_root"
    REMOTE_REPO_DIR="hololive-bot"
    source_snapshot=/dev/null
    backup_dir="backups/ap-test-20260926T000000Z"
    containers_list="hololive-youtube-collector-a hololive-youtube-collector-b"
    cutover_armed="$cutover"
    source_armed="$source"
    rollback_available="$rollback"
    restore_after_failed_deploy "$status"
  ) 2>"$stderr_log"
}

# 기준점이 있는 재배포 실패: 기록된 backup으로 이전 collector와 issuer를 함께 되돌린다(stack-audit T05, paired rollback).
rc=0
run_failed_deploy true true true 7 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "redeploy failure must keep the original exit status (got $rc)"
grep -Fqx 'BACKUP_DIR=backups/ap-test-20260926T000000Z APPROVE=true args=ap-test --apply' "$rollback_log" \
  || fail "redeploy failure must apply the recorded paired rollback: $(cat "$rollback_log")"
[[ ! -s "$remote_log" ]] || fail "redeploy failure must not stop containers directly: $(cat "$remote_log")"

# 기준점이 없는 첫 배포 실패: 새 collector와 issuer를 멈추고 비활성을 확인하며, 거절될 ap-rollback.sh를 부르지 않는다.
rc=0
run_failed_deploy true true false 7 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "first deploy failure must keep the original exit status (got $rc)"
[[ ! -s "$rollback_log" ]] || fail "first deploy failure must not run ap-rollback.sh without rollback-image-tag"
grep -Fq 'for container in hololive-youtube-collector-a hololive-youtube-collector-b hololive-youtube-po-b; do' "$remote_log" \
  || fail "first deploy failure must act on every deployed collector and the issuer: $(cat "$remote_log")"
grep -Fq 'docker stop "$container"' "$remote_log" \
  || fail "first deploy failure must stop the new containers"
grep -Fq 'container still active' "$remote_log" \
  || fail "first deploy failure must require the new containers to be inactive"
if grep -Eq '\|\| true|2>/dev/null' "$remote_log"; then
  fail "first deploy stop must not hide docker failures: $(cat "$remote_log")"
fi
grep -Fq 'fix forward' "$stderr_log" || fail "first deploy failure must tell the operator to fix forward"

# 정지 확인이 실패하면 원래 종료 상태를 유지하고 정지되지 않았음을 드러낸다.
rc=0
run_failed_deploy true true false 7 1 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "failed stop must keep the original exit status (got $rc)"
grep -Fq 'could not be confirmed stopped' "$stderr_log" \
  || fail "failed stop must report that the new containers may still be running"
if grep -Fq 'new collector and issuer stopped' "$stderr_log"; then
  fail "failed stop must not claim the containers stopped"
fi

# cutover 전 실패는 staging 전 source snapshot만 복원한다.
rc=0
run_failed_deploy false true false 7 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "pre-cutover failure must keep the original exit status (got $rc)"
grep -Fq 'python3 - restore' "$remote_log" || fail "pre-cutover failure must restore the captured source snapshot"
[[ ! -s "$rollback_log" ]] || fail "pre-cutover failure must not run ap-rollback.sh"

rc=0
run_failed_deploy false false false 7 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "disarmed handler must keep the original exit status (got $rc)"
[[ ! -s "$remote_log" && ! -s "$stderr_log" && ! -s "$rollback_log" ]] \
  || fail "disarmed handler must not touch the AP host or print rollback guidance"

# 기준점 판정은 ap-rollback.sh와 같은 파일을 읽고, 값 검증까지 cutover를 arm하기 전에 끝나야 한다.
grep -Fq "if [[ -r '\$backup_dir/rollback-image-tag' ]]; then printf '%s\\n' true; else printf '%s\\n' false; fi" "$deploy_script" \
  || fail "ap-deploy.sh must derive rollback_available from the recorded rollback image tag"
# shellcheck disable=SC2016 # ap-deploy.sh의 문자 그대로인 줄을 찾는다.
probe_line="$(grep -n '^rollback_available="\$($' "$deploy_script" | cut -d: -f1)"
# shellcheck disable=SC2016 # ap-deploy.sh의 문자 그대로인 줄을 찾는다.
validate_line="$(grep -n '^case "\$rollback_available" in$' "$deploy_script" | cut -d: -f1)"
arm_line="$(grep -n '^cutover_armed=true$' "$deploy_script" | cut -d: -f1)"
[[ -n "$probe_line" && -n "$validate_line" && -n "$arm_line" ]] \
  || fail "ap-deploy.sh must probe and validate rollback_available before arming the cutover"
[[ "$probe_line" -lt "$validate_line" && "$validate_line" -lt "$arm_line" ]] \
  || fail "rollback_available must be probed and validated before the cutover is armed"

# 퇴역 producer 상태 기록·복원과 repo 루트 compose 경로 폴백은 삭제했다(stack-audit 2026-09-26 T11).
for script in "$deploy_script" "$rollback_script" "$rollback_remote_script"; do
  if grep -Eq 'retired-producer-cutover\.sh|restore_retired_producer|first_cutover|first-cutover|_LEGACY_FILE' "$script"; then
    fail "$(basename "$script") must not keep the retired producer cutover or legacy compose path fallback"
  fi
done
grep -Fq "backup has no rollback-image-tag" "$rollback_remote_script" \
  || fail "AP rollback must refuse backups without a recorded collector rollback image"

echo "all AP cutover failure trap checks passed"
