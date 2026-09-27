#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
deploy_script="$ROOT_DIR/scripts/deploy/ap-deploy.sh"
rollback_script="$ROOT_DIR/scripts/deploy/ap-rollback.sh"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
remote_log="$tmp/remote.log"
stderr_log="$tmp/stderr.log"

trap_body="$(sed -n '/^handle_failed_collector_deploy() {$/,/^}$/p' "$deploy_script")"
[[ -n "$trap_body" ]] || fail "ap-deploy.sh must define handle_failed_collector_deploy"

# ERR trap 함수를 실패 상태로 실행하고 원격 명령을 기록한다. 실제 원격 접속은 하지 않는다.
# remote_status는 원격 정지 명령이 실패한 경우를 흉내 낸다.
run_failed_cutover() {
  local armed="$1"
  local rollback="$2"
  local status="$3"
  local remote_status="${4:-0}"

  : > "$remote_log"
  : > "$stderr_log"
  # shellcheck disable=SC2034,SC2329 # eval로 불러온 trap 함수가 아래 변수와 remote 스텁을 읽는다.
  (
    remote() {
      printf '%s\n' "$*" >> "$remote_log"
      return "$remote_status"
    }
    eval "$trap_body"
    AP_NAME="ap-test"
    backup_dir="backups/ap-test-20260926T000000Z"
    containers_list="hololive-youtube-collector-a hololive-youtube-collector-b"
    cutover_armed="$armed"
    rollback_available="$rollback"
    set +e
    (exit "$status")
    handle_failed_collector_deploy
  ) 2>"$stderr_log"
}

# 기준점이 있는 재배포 실패: collector를 배포된 상태로 두고 기록된 rollback으로 안내한다(stack-audit T05).
rc=0
run_failed_cutover true true 7 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "redeploy failure must keep the original exit status (got $rc)"
[[ ! -s "$remote_log" ]] || fail "redeploy failure with a rollback point must leave the deployed collector untouched: $(cat "$remote_log")"
grep -Fq "BACKUP_DIR='backups/ap-test-20260926T000000Z' ./scripts/deploy/ap-rollback.sh ap-test --apply" "$stderr_log" \
  || fail "redeploy failure must point the operator to the recorded collector rollback"

# 기준점이 없는 첫 배포 실패: 새 collector를 멈추고 비활성을 확인하며, 거절될 ap-rollback.sh를 안내하지 않는다.
rc=0
run_failed_cutover true false 7 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "first deploy failure must keep the original exit status (got $rc)"
grep -Fq 'for container in hololive-youtube-collector-a hololive-youtube-collector-b; do' "$remote_log" \
  || fail "first deploy failure must act on every deployed collector container: $(cat "$remote_log")"
grep -Fq 'docker stop "$container"' "$remote_log" \
  || fail "first deploy failure must stop the new collector"
grep -Fq 'collector container still active' "$remote_log" \
  || fail "first deploy failure must require the new collector to be inactive"
if grep -Eq '\|\| true|2>/dev/null' "$remote_log"; then
  fail "first deploy stop must not hide docker failures: $(cat "$remote_log")"
fi
grep -Fq 'fix forward' "$stderr_log" || fail "first deploy failure must tell the operator to fix forward"
if grep -Fq 'ap-rollback.sh' "$stderr_log"; then
  fail "first deploy failure must not point to ap-rollback.sh, which refuses backups without rollback-image-tag"
fi

# 정지 확인이 실패하면 원래 종료 상태를 유지하고 정지되지 않았음을 드러낸다.
rc=0
run_failed_cutover true false 7 1 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "failed stop must keep the original exit status (got $rc)"
grep -Fq 'could not be confirmed stopped' "$stderr_log" \
  || fail "failed stop must report that the new collector may still be running"
if grep -Fq 'new collector stopped' "$stderr_log"; then
  fail "failed stop must not claim the collector stopped"
fi

rc=0
run_failed_cutover false false 7 || rc="$?"
[[ "$rc" -eq 7 ]] || fail "disarmed trap must keep the original exit status (got $rc)"
[[ ! -s "$remote_log" && ! -s "$stderr_log" ]] || fail "disarmed trap must not touch the AP host or print rollback guidance"

# 기준점 판정은 ap-rollback.sh와 같은 파일을 읽고, 값 검증까지 trap을 걸기 전에 끝나야 한다.
grep -Fq "if [[ -r '\$backup_dir/rollback-image-tag' ]]; then printf '%s\\n' true; else printf '%s\\n' false; fi" "$deploy_script" \
  || fail "ap-deploy.sh must derive rollback_available from the recorded rollback image tag"
# shellcheck disable=SC2016 # ap-deploy.sh의 문자 그대로인 줄을 찾는다.
probe_line="$(grep -n '^rollback_available="\$($' "$deploy_script" | cut -d: -f1)"
# shellcheck disable=SC2016 # ap-deploy.sh의 문자 그대로인 줄을 찾는다.
validate_line="$(grep -n '^case "\$rollback_available" in$' "$deploy_script" | cut -d: -f1)"
arm_line="$(grep -n '^cutover_armed=true$' "$deploy_script" | cut -d: -f1)"
trap_line="$(grep -n '^trap handle_failed_collector_deploy ERR$' "$deploy_script" | cut -d: -f1)"
[[ -n "$probe_line" && -n "$validate_line" && -n "$arm_line" && -n "$trap_line" ]] \
  || fail "ap-deploy.sh must probe and validate rollback_available and arm the ERR trap"
[[ "$probe_line" -lt "$validate_line" && "$validate_line" -lt "$arm_line" && "$arm_line" -lt "$trap_line" ]] \
  || fail "rollback_available must be probed and validated before the ERR trap is armed"

# 퇴역 producer 상태 기록·복원과 repo 루트 compose 경로 폴백은 삭제했다(stack-audit 2026-09-26 T11).
for script in "$deploy_script" "$rollback_script"; do
  if grep -Eq 'lib/retired-producer-cutover\.sh|restore_retired_producer|first_cutover|_LEGACY_FILE' "$script"; then
    fail "$(basename "$script") must not keep the retired producer cutover or legacy compose path fallback"
  fi
done
grep -Fq "backup has no rollback-image-tag" "$rollback_script" \
  || fail "ap-rollback.sh must refuse backups without a recorded collector rollback image"

echo "all AP cutover failure trap checks passed"
