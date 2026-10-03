# shellcheck shell=bash
# native 배포의 변경 결과만 기록합니다. argv·출력·환경 값은 복구 기록에 남기지 않습니다.
native_recovery_dir=/opt/hololive-bot/youtube-collector/cutover-recovery

native_recovery_require_clear() {
  local guard_status=0
  sudo -n test ! -e "$native_recovery_dir" || guard_status=$?
  if (( guard_status != 0 )); then
    echo "native cutover recovery guard exists; verify the recorded worker and runtime before another apply: $native_recovery_dir" >&2
    return 1
  fi
}

native_cutover_claim() {
  # 원자적 mkdir가 동시 배포와 미확정 이전 변경을 함께 거절합니다.
  sudo -n mkdir -m 0700 "$native_recovery_dir"
  sudo -n sh -c 'umask 077; printf "%s\n" "$1" > "$2/release"; printf "%s\n" "$3" > "$2/owner-pid"' \
    sh "$1" "$native_recovery_dir" "$BASHPID"
}

native_cutover_signal() {
  native_signal_status="$1"
  # wait가 중단됐을 때는 실행 중인 worker의 완료를 먼저 확인합니다.
  if [[ "${native_phase_active:-false}" != true && "${native_restoring:-false}" != true ]]; then
    native_restoring=true
    restore_native_after_failed_cutover "$1"
  fi
}

native_cutover_unknown() {
  # shellcheck disable=SC2034 # 연결된 remote apply가 복원 허용 여부를 읽습니다.
  native_outcome_unknown=true
  sudo -n sh -c 'umask 077; printf "outcome_unknown\n" > "$1/outcome"' sh "$native_recovery_dir" || \
    echo 'failed to write native outcome record; recovery guard retained' >&2
  echo "native cutover outcome_unknown; recovery guard, release and rollback records retained: $native_recovery_dir" >&2
}

native_cutover_release_guard() {
  sudo -n rm -r -- "$native_recovery_dir"
}

native_cutover_run() {
  local step="$1" worker_status=0 receipt_status="" signal_wait=0 monitor_was_set=false
  shift
  native_phase_active=true
  native_phase_started=false
  native_completion_verified=false
  native_worker_status=""
  sudo -n rm -f "$native_recovery_dir/terminal" || {
    worker_status=$?; native_worker_status="$worker_status"; native_phase_active=false; return "$worker_status"
  }
  sudo -n sh -c 'umask 077; printf "%s\n" "$1" > "$2/step"' sh "$step" "$native_recovery_dir" || {
    worker_status=$?; native_worker_status="$worker_status"; native_phase_active=false; return "$worker_status"
  }
  if (( ${native_signal_status:-0} != 0 )); then
    native_worker_status="$native_signal_status"
    native_phase_active=false
    return "$native_signal_status"
  fi
  [[ "$-" != *m* ]] || monitor_was_set=true
  # job control가 worker에 별도 소유 process group과 살아 있는 SIGINT를 줍니다.
  set -m
  # shellcheck disable=SC2034 # 연결된 ERR handler가 실행 중 변경의 미확정 상태를 읽습니다.
  native_phase_started=true
  (
    set -Eeuo pipefail
    set +m
    trap - ERR
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    native_worker_terminal() {
      local terminal_status="$?"
      if (( terminal_status < 128 )); then
        sudo -n sh -c 'umask 077; printf "%s\n" "$1" > "$2/terminal"' sh "$terminal_status" "$native_recovery_dir"
      fi
    }
    trap native_worker_terminal EXIT
    "$@"
  ) &
  native_worker_pid=$!
  sudo -n sh -c 'umask 077; printf "%s\n" "$1" > "$2/worker-pid"' sh "$native_worker_pid" "$native_recovery_dir" || {
    worker_status=$?
    kill -TERM -- "-$native_worker_pid" 2>/dev/null || true
    native_cutover_unknown
    return "$worker_status"
  }
  # 조건 문맥은 wait에만 적용합니다. worker의 실제 함수는 set -e를 유지합니다.
  wait "$native_worker_pid" || worker_status=$?
  if (( ${native_signal_status:-0} != 0 )); then
    # 부모에만 신호가 오면 worker는 계속 실행됩니다. 완료 관찰은 5초에 한정합니다.
    for (( signal_wait=0; signal_wait<50; signal_wait++ )); do
      kill -0 "$native_worker_pid" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$native_worker_pid" 2>/dev/null; then
      # 이 배포가 만든 worker group에만 전달합니다. daemon 결과를 복원 성공으로 추정하지 않습니다.
      kill -TERM -- "-$native_worker_pid" 2>/dev/null || true
      native_cutover_unknown
      native_worker_pid=""
      $monitor_was_set || set +m
      return "${native_signal_status}"
    fi
    worker_status=0
    wait "$native_worker_pid" || worker_status=$?
  fi
  $monitor_was_set || set +m
  # shellcheck disable=SC2034 # 복원 terminal status와 요청 신호 상태를 분리합니다.
  native_worker_status="$worker_status"
  if sudo -n test -f "$native_recovery_dir/terminal"; then
    receipt_status="$(sudo -n cat "$native_recovery_dir/terminal")" || {
      worker_status=$?
      native_cutover_unknown
      return "$worker_status"
    }
  fi
  if (( worker_status >= 128 )) || [[ "$receipt_status" != "$worker_status" ]]; then
    native_cutover_unknown
    native_worker_pid=""
    native_phase_active=false
    (( ${native_signal_status:-0} == 0 )) || return "$native_signal_status"
    (( worker_status != 0 )) || worker_status=1
    return "$worker_status"
  fi
  # shellcheck disable=SC2034 # restore는 wait와 기록 검증을 모두 마친 실제 성공만 guard 해제로 인정합니다.
  native_completion_verified=true
  native_worker_pid=""
  native_phase_active=false
  (( ${native_signal_status:-0} == 0 )) || return "$native_signal_status"
  return "$worker_status"
}
