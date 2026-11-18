# shellcheck shell=bash
native_recovery_dir=/opt/hololive-bot/youtube-collector/cutover-recovery
native_claim_nonce=""

native_capture_collector_state() {
  local unit="$1" previous="$2" load persistent status=0
  load="$(systemctl show "$unit" -p LoadState --value)" || status=$?
  if [[ -z "$previous" && "$load" == not-found ]] && (( status == 0 || status == 4 )); then
    collector_prechange_state=not-found
    collector_prechange_enabled=false
    collector_prechange_running=false
    return 0
  fi
  [[ -n "$previous" && "$load" == loaded && "$status" == 0 ]] || return 1
  collector_prechange_state="$(systemctl show "$unit" -p ActiveState --value)" || return 1
  persistent="$(systemctl show "$unit" -p UnitFileState --value)" || return 1
  case "$collector_prechange_state" in
    active|activating|reloading) collector_prechange_running=true ;;
    inactive|failed)
      # shellcheck disable=SC2034
      collector_prechange_running=false ;;
    *) return 1 ;;
  esac
  case "$persistent" in
    enabled) collector_prechange_enabled=true ;;
    disabled)
      # shellcheck disable=SC2034
      collector_prechange_enabled=false ;;
    *) return 1 ;;
  esac
}

native_recovery_require_clear() {
  local guard_status=0
  sudo -n test ! -e "$native_recovery_dir" || guard_status=$?
  if (( guard_status != 0 )); then
    echo "native cutover recovery guard exists; verify the recorded worker and runtime before another apply: $native_recovery_dir" >&2
    return 1
  fi
}

native_cutover_claim() {
  local nonce
  native_claim_nonce=""
  IFS= read -r nonce < /proc/sys/kernel/random/uuid || return 1
  [[ "$nonce" =~ ^[0-9a-f-]{36}$ ]] || return 1
  sudo -n mkdir -m 0700 "$native_recovery_dir" || return
  sudo -n sh -ec 'umask 077; printf "%s\n" "$1" > "$2/release"; printf "%s\n" "$3" > "$2/owner-pid"; printf "%s\n" "$4" > "$2/claim"' \
    sh "$1" "$native_recovery_dir" "$BASHPID" "$nonce" || return
  native_claim_nonce="$nonce"
}

native_cutover_signal() {
  native_signal_status="$1"
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
  [[ -n "${native_claim_nonce:-}" && "${native_outcome_unknown:-false}" != true ]] || return 1
  sudo -n sh -ec '
    test ! -L "$1" && cd "$1" || exit 1
    test "$(cat claim)" = "$2" && test "$(cat owner-pid)" = "$3" || exit 1
    test ! -e outcome || exit 1
    for item in ./* ./.[!.]* ./..?*; do
      test ! -L "$item" || exit 1
      test -e "$item" || continue
      case "$item" in
        ./step|./terminal|./worker-pid|./release|./owner-pid|./claim) test -f "$item" || exit 1 ;;
        *) exit 1 ;;
      esac
    done
    rm -f -- step terminal worker-pid release owner-pid claim
    rmdir -- "$1"
  ' sh "$native_recovery_dir" "$native_claim_nonce" "$BASHPID" || return
  native_claim_nonce=""
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
  wait "$native_worker_pid" || worker_status=$?
  if (( ${native_signal_status:-0} != 0 )); then
    for (( signal_wait=0; signal_wait<50; signal_wait++ )); do
      kill -0 "$native_worker_pid" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$native_worker_pid" 2>/dev/null; then
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
