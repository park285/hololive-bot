# shellcheck shell=bash
native_recovery_dir="${slot_root:?collector slot preamble required}/cutover-recovery"
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
  sudo -n sh -ec '
    test ! -L "$1"
    cd "$1"
    test -n "$2" && test "$(cat claim)" = "$2" || exit 1
    test ! -L outcome
    if test -e outcome; then
      test -f outcome && test "$(cat outcome)" = outcome_unknown
    else
      umask 077; set -C; printf "outcome_unknown\n" > outcome
    fi
  ' sh "$native_recovery_dir" "$native_claim_nonce" || echo 'could not record outcome on owned native guard; retained' >&2
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

native_logrotate_body() {
  cat <<'LOGROTATE'
/var/log/hololive-bot/*.log {
    size 10M
    rotate 14
    missingok
    notifempty
    olddir /var/log/hololive-bot/archive
    compress
    delaycompress
    copytruncate
}
LOGROTATE
}

native_udp_body() { printf 'net.core.rmem_max = %s\nnet.core.wmem_max = %s\n' "$1" "$1"; }
native_swap_body() { printf 'vm.swappiness = 10\n'; }

native_existing_host_check() {
  local buffer="$1" mib="$2"
  : "${swapfile:?}" "${releases_root:?}" "${host_env:?}" "${unit_file:?}" "${po_unit_file:?}" "${po_socket_file:?}"
  native_logrotate_body | sudo -n cmp -s - /etc/logrotate.d/hololive-bot || return 1
  native_udp_body "$buffer" | sudo -n cmp -s - /etc/sysctl.d/99-hololive-quic-udp-buffer.conf || return 1
  native_swap_body | sudo -n cmp -s - /etc/sysctl.d/99-hololive-swap.conf || return 1
  local live swap
  live="$(sysctl -n net.core.rmem_max net.core.wmem_max vm.swappiness)" || return 1
  [[ "$live" == "$buffer"$'\n'"$buffer"$'\n10' ]] || return 1
  swap="$(swapon --noheadings --raw --show=NAME)" || return 1
  grep -Fxq "$swapfile" <<< "$swap" || return 1
  sudo -n file -b "$swapfile" | grep -F 'swap file' >/dev/null || return 1
  sudo -n python3 -I -B - "$mib" "$swapfile" "$releases_root" "$host_env" "$unit_file" "$po_unit_file" "$po_socket_file" <<'PY'
import grp, os, pwd, stat, sys
from pathlib import Path
mib, swap, releases, env, *units = sys.argv[1:]
user, group = pwd.getpwnam('hololive'), grp.getgrnam('opc')
assert user.pw_uid != 0 and group.gr_gid != 0 and user.pw_gid == group.gr_gid
assert user.pw_dir == '/nonexistent' and user.pw_shell == '/usr/sbin/nologin'
def check(path, kind, uid=0, gid=0, mode=None):
    s = os.lstat(path)
    assert kind(s.st_mode) and (s.st_uid, s.st_gid) == (uid, gid)
    assert (s.st_mode & 0o022 == 0) if mode is None else (stat.S_IMODE(s.st_mode) == mode)
    return s
for d in (releases, '/var/lib/hololive-bot', '/etc/sysctl.d'):
    check(d, stat.S_ISDIR, mode=0o755)
check('/etc/hololive-bot', stat.S_ISDIR, mode=0o750)
for d in ('/var/log/hololive-bot', '/var/log/hololive-bot/archive'):
    check(d, stat.S_ISDIR, user.pw_uid, group.gr_gid, 0o750)
for f in ('/etc/logrotate.d/hololive-bot', '/etc/sysctl.d/99-hololive-quic-udp-buffer.conf',
          '/etc/sysctl.d/99-hololive-swap.conf', '/etc/fstab'):
    check(f, stat.S_ISREG)
assert check(swap, stat.S_ISREG, mode=0o600).st_size == int(mib)*1048576
rows = [line.partition(b'#')[0].split() for line in Path('/etc/fstab').read_bytes().splitlines()]
assert [r for r in rows if r[:1] == [os.fsencode(swap)]] == [[os.fsencode(swap), b'none', b'swap', b'sw', b'0', b'0']]
check(env, stat.S_ISREG, mode=0o640)
for f in units:
    check(f, stat.S_ISREG, mode=0o644)
PY
}

native_release_pin() {
  sudo -n python3 -I -B - "$@" <<'PY'
import hashlib, os, stat, sys, tempfile
from pathlib import Path
action, kind, live, old, new, expected = sys.argv[1:]
live, old, new = map(Path, (live, old, new))
key, suffix = {'profile': (b'STACK_WORKER_PROFILE_FILE=', 'worker-profile.json'),
               'issuer': (b'RootDirectory=', 'po-sandbox/rootfs')}[kind]
def line(release):
    return key + os.fsencode(release / suffix) + b'\n'
original = (old / live.name).read_bytes()
lines = original.splitlines(keepends=True)
assert [s for s in lines if s.startswith(key)] == [line(old)]
lines[lines.index(line(old))] = line(new)
candidate = b''.join(lines)
assert hashlib.sha256(candidate).hexdigest() == expected
s = live.lstat()
assert stat.S_ISREG(s.st_mode) and s.st_nlink == 1 and (s.st_uid, s.st_gid) == (os.geteuid(), os.getegid())
assert stat.S_IMODE(s.st_mode) == (0o640 if kind == 'profile' else 0o644)
current = live.read_bytes()
if action == 'check':
    sys.exit(0 if current == original else 1)
assert action in ('apply', 'restore', 'recoverable') and (new / live.name).read_bytes() == candidate
if action != 'apply':
    assert current in (original, candidate)
    if current == original or action == 'recoverable':
        sys.exit(0)
else:
    assert current == original
with tempfile.NamedTemporaryFile(dir=live.parent, delete=False) as tmp:
    name = tmp.name
    try:
        tmp.write(candidate if action == 'apply' else original)
        tmp.flush()
        os.fchown(tmp.fileno(), s.st_uid, s.st_gid)
        os.fchmod(tmp.fileno(), stat.S_IMODE(s.st_mode))
        attrs = set(os.listxattr(live))
        for attr in set(os.listxattr(name)) - attrs:
            os.removexattr(name, attr)
        for attr in attrs:
            value = os.getxattr(live, attr)
            if attr not in os.listxattr(name) or os.getxattr(name, attr) != value:
                os.setxattr(name, attr, value)
        assert stat.S_IMODE(os.fstat(tmp.fileno()).st_mode) == stat.S_IMODE(s.st_mode)
        os.utime(tmp.fileno(), ns=(s.st_atime_ns, s.st_mtime_ns))
        os.fsync(tmp.fileno())
        observed = live.lstat()
        assert (observed.st_dev, observed.st_ino, observed.st_ctime_ns) == (s.st_dev, s.st_ino, s.st_ctime_ns)
        os.replace(name, live)
        d = os.open(live.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(d)
        finally:
            os.close(d)
    finally:
        if os.path.lexists(name):
            os.unlink(name)
PY
}

native_file_digest() {
  local sum
  sum="$(sha256sum -- "$1")" || return 1
  printf '%s\n' "${sum%% *}"
}
