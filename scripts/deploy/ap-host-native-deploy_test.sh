#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="${ROOT_DIR}/scripts/deploy/ap-host-native-deploy.sh"
REMOTE_APPLY="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-remote-apply.sh"
ROLLBACK="${ROOT_DIR}/scripts/deploy/ap-host-native-rollback.sh"
RELEASE_PATH_LIB="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-release-path.sh"
ROLLBACK_CHECK_LIB="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-rollback-check.sh"
UNIT_TEMPLATE="${ROOT_DIR}/scripts/deploy/lib/hololive-youtube-collector.service"

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

# shellcheck source=scripts/deploy/lib/ap-host-native-release-path.sh
. "${RELEASE_PATH_LIB}"
# shellcheck source=scripts/deploy/lib/ap-host-native-rollback-check.sh
. "${ROLLBACK_CHECK_LIB}"

if grep -Eq 'EnvironmentFile=-?/etc/stack-secrets/hololive-bot/(ap-)?compose\.env' "${UNIT_TEMPLATE}"; then
  record_fail "checked-in host-native unit template must not load a shared Compose env"
elif ! grep -Fxq 'EnvironmentFile=/etc/stack-secrets/hololive-bot/youtube-collector.env' "${UNIT_TEMPLATE}" ||
     ! grep -Fxq 'EnvironmentFile=/etc/hololive-bot/youtube-collector-host.env' "${UNIT_TEMPLATE}"; then
  record_fail "checked-in host-native unit template must require the two scoped env files"
else
  pass "checked-in host-native unit template exposes only collector-scoped env files"
fi

write_host_env_fn="$(awk '/^write_host_env\(\) \{/,/^}$/' "${DEPLOY}")"
cfg008_dir="$(mktemp -d)"
generated_env="${cfg008_dir}/youtube-collector-host.env"
if [[ -z "${write_host_env_fn}" ]]; then
  record_fail "ap-host-native write_host_env function is missing"
elif (
  export service="youtube-collector-a"
  export port="30005"
  export AP_POSTGRES_HOST="100.100.1.8"
  export AP_POSTGRES_PORT="5433"
  export AP_SSH_HOST="100.100.1.6"
  eval "${write_host_env_fn}"
  write_host_env "${generated_env}"
) && [[ -s "${generated_env}" ]]; then
  if grep -Eq '^(CACHE_HOST|CACHE_PORT|CACHE_SOCKET_PATH|CACHE_PASSWORD|CACHE_DB)=' "${generated_env}"; then
    record_fail "generated host env contents still include CACHE lines"
  elif grep -Eq '^SETTINGS_DIR=' "${generated_env}"; then
    record_fail "generated host env contents still include SETTINGS_DIR"
  elif ! grep -Fxq 'POSTGRES_HOST=100.100.1.8' "${generated_env}" ||
       ! grep -Fxq 'POSTGRES_PORT=5433' "${generated_env}"; then
    record_fail "generated host env must use the direct Osaka PostgreSQL endpoint"
  elif ! grep -Eq '^HOLOLIVE_INTERNAL_H3_CA_CERT_FILE=.+' "${generated_env}" ||
       ! grep -Eq '^HOLOLIVE_INTERNAL_H3_SERVER_NAME=.+' "${generated_env}"; then
    record_fail "generated host env must emit both dedicated internal H3 client keys"
  else
    pass "generated host env contents have 0 CACHE lines"
    pass "generated host env contents have 0 SETTINGS_DIR lines"
    pass "generated host env emits the dedicated internal H3 client keys"
  fi
else
  record_fail "ap-host-native write_host_env did not produce generated env contents"
fi
rm -rf "${cfg008_dir}"

permission_fn="$(awk '/^normalize_runtime_payload_permissions\(\) \{/,/^}$/' "${REMOTE_APPLY}")"
permission_fixture="$(mktemp -d)"
if [[ -z "${permission_fn}" ]]; then
  record_fail "host-native payload permission normalizer is missing"
elif (
  sudo() {
    [[ "${1:-}" != "-n" ]] || shift
    # 운영 AP의 구형 coreutils에서도 실행 가능한 명령이어야 합니다.
    if [[ "${1:-}" == chmod && " $* " == *" -P "* ]]; then
      return 1
    fi
    command "$@"
  }

  normal="${permission_fixture}/normal"
  external="${permission_fixture}/external"
  mkdir -p "${normal}/youtubejs/src" "${external}"
  printf 'export {}\n' >"${normal}/youtubejs/src/server.mjs"
  printf 'sentinel\n' >"${external}/sentinel"
  chmod 0700 "${normal}/youtubejs" "${normal}/youtubejs/src" "${external}"
  chmod 0600 "${normal}/youtubejs/src/server.mjs" "${external}/sentinel"
  ln -s "${external}" "${normal}/youtubejs/external-link"
  ln -s "${external}/sentinel" "${normal}/youtubejs/external-file"

  eval "${permission_fn}"
  normalize_runtime_payload_permissions "${normal}" || exit 1
  [[ "$(stat -c '%a' "${normal}/youtubejs/src/server.mjs")" == 644 ]] || exit 1
  [[ "$(stat -c '%a' "${external}/sentinel")" == 600 ]] || exit 1

  bad="${permission_fixture}/bad"
  mkdir -p "${bad}"
  ln -s "${external}" "${bad}/youtubejs"
  if normalize_runtime_payload_permissions "${bad}"; then
    exit 1
  fi
  [[ "$(stat -c '%a' "${external}/sentinel")" == 600 ]]
); then
  pass "host-native payload permissions survive restrictive umask without following symlinks"
else
  record_fail "host-native payload permissions must cover parent traversal and reject symlink escape"
fi
rm -rf "${permission_fixture}"

if grep -Fq 'ReadWritePaths=/var/lib/hololive-bot' "${UNIT_TEMPLATE}"; then
  pass "ap-host-native keeps /var/lib/hololive-bot writable under systemd hardening"
else
  record_fail "ap-host-native must keep /var/lib/hololive-bot writable"
fi

if grep -q '^ReadWritePaths=.*stack-secrets' "${UNIT_TEMPLATE}"; then
  record_fail "ap-host-native must not grant write access to the static secret directory"
else
  pass "ap-host-native keeps /etc/stack-secrets read-only under ProtectSystem=strict"
fi

unit_copy_dir="$(mktemp -d)"
mkdir -p "${unit_copy_dir}/etc/systemd/system" \
  "${unit_copy_dir}/opt/hololive-bot/youtube-collector/current/bin"
cp "${UNIT_TEMPLATE}" "${unit_copy_dir}/etc/systemd/system/hololive-youtube-collector@.service"
printf '#!/bin/sh\nexit 0\n' >"${unit_copy_dir}/opt/hololive-bot/youtube-collector/current/bin/youtube-collector-wrapper"
chmod +x "${unit_copy_dir}/opt/hololive-bot/youtube-collector/current/bin/youtube-collector-wrapper"
for fixture_target in sysinit.target basic.target network-online.target shutdown.target sockets.target paths.target; do
  printf '[Unit]\nDescription=fixture\n' >"${unit_copy_dir}/etc/systemd/system/${fixture_target}"
done
if systemd-analyze verify --root="${unit_copy_dir}" /etc/systemd/system/hololive-youtube-collector@.service; then
  pass "canonical host-native systemd unit passes systemd-analyze verify"
else
  record_fail "canonical host-native systemd unit must pass systemd-analyze verify"
fi

rm -rf "${unit_copy_dir}"

bash "${ROOT_DIR}/scripts/deploy/ap-host-native-collector-wrapper_test.sh"
bash "${ROOT_DIR}/scripts/deploy/collector-cutover-failure_test.sh"
native_test_runtime="/run/user/$(id -u)"
test -S "${native_test_runtime}/bus"
env XDG_RUNTIME_DIR="${native_test_runtime}" DBUS_SESSION_BUS_ADDRESS="unix:path=${native_test_runtime}/bus" \
  systemd-run --user --quiet --wait --pipe --collect \
  -p Type=exec -p RuntimeMaxSec=45s -p KillMode=control-group \
  -p "WorkingDirectory=${ROOT_DIR}" \
  bash "${ROOT_DIR}/scripts/deploy/ap-host-native-cutover_test.sh"

native_units_fns="$(awk '/^stop_collector_unit_and_require_inactive\(\) \{/,/^}$/; /^stop_native_units_and_require_inactive\(\) \{/,/^}$/; /^native_restore_recorded_runtime\(\) \{/,/^}$/; /^restore_native_after_failed_cutover\(\) \{/,/^}$/; /^native_cutover_failed\(\) \{/,/^}$/; /^arm_native_cutover_restore\(\) \{/,/^}$/' "${REMOTE_APPLY}")"
# cutover 최상위 정지 단계: 복원 ERR trap 설치부터 새 release의 첫 설치 변경 직전까지다.
cutover_stop_step='arm_native_cutover_restore; native_cutover_run stop stop_native_units_and_require_inactive'
stop_fixture="$(mktemp -d)"
trap 'rm -rf "${stop_fixture}"' EXIT
# 가짜 systemctl은 active unit을 파일로 두고 호출을 기록한다. socket stop은 Requires=처럼 service도 멈춘다.
# collector_sticks가 있으면 disable --now 뒤에도 collector가 active로 남는다. sudo는 systemctl만 실행하고
# 나머지 host 변경(install/ln/rm)은 기록만 한다. po_restore_fails가 있으면 이전 PO 복원이 실패한다.
run_native_units() (
  local state="$1" script="$2"
  po_service=hololive-youtube-po.service
  po_socket=hololive-youtube-po.socket
  unit=hololive-youtube-collector@youtube-collector-a.service
  old_target="${state}/old-release"
  # shellcheck disable=SC2034 # eval한 실제 복원 함수가 읽는 경로다.
  host_env="${state}/host.env" unit_file="${state}/unit" current_link="${state}/current"
  # shellcheck disable=SC2034 # eval한 실제 복원 함수가 읽는 경로입니다.
  releases_root="${state}/releases" previous_link="${state}/previous"
  mkdir -p "${old_target}"
  # 기존 unit 순서 fixture는 실제 명령 완료를 동기 실행합니다. 신호/receipt는 별도 실제 child 회귀가 소유합니다.
  native_cutover_run() {
    shift
    # shellcheck disable=SC2034 # eval한 실제 복원 함수가 읽는 완료 상태입니다.
    native_completion_verified=true
    ( set -e; "$@"; )
  }
  native_cutover_release_guard() { :; }
  sudo() {
    [[ "${1:-}" != "-n" ]] || shift
    if [[ "$1" == systemctl ]]; then
      "$@"
    else
      printf 'sudo %s\n' "$*" >>"${state}/calls"
    fi
  }
  systemctl() {
    local target
    printf '%s\n' "$*" >>"${state}/calls"
    case "$1" in
      cat | daemon-reload | enable) return 0 ;;
      is-active) [[ -e "${state}/active/${*: -1}" ]] ;;
      stop | disable)
        [[ "$1" == stop ]] || shift
        shift
        for target in "$@"; do
          [[ "${target}" == "${unit}" && -e "${state}/collector_sticks" ]] && continue
          rm -f "${state}/active/${target}"
          [[ "${target}" != "${po_socket}" ]] || rm -f "${state}/active/${po_service}"
        done
        ;;
      *) return 1 ;;
    esac
  }
  native_previous_link_restore() { printf 'native_previous_link_restore\n' >>"${state}/calls"; }
  po_restore_previous() {
    printf 'po_restore_previous %s\n' "$*" >>"${state}/calls"
    [[ ! -e "${state}/po_restore_fails" ]]
  }
  eval "${native_units_fns}"
  eval "${script}"
)
run_native_stop() { run_native_units "$1" stop_native_units_and_require_inactive; }
# 원격 script처럼 set -e가 살아 있도록 조건(if/!/&&/||) 밖에서 실행하고 종료 상태를 native_rc에 남긴다.
run_native_case() {
  set +e
  run_native_units "$1" "$2" >"$1/stdout" 2>"$1/stderr"
  native_rc=$?
  set -e
}
stop_case() {
  local state="${stop_fixture}/$1" unit_name
  shift
  mkdir -p "${state}/active"
  for unit_name in "$@"; do
    touch "${state}/active/${unit_name}"
  done
  printf '%s\n' "${state}"
}
running_units=(hololive-youtube-po.socket hololive-youtube-po.service hololive-youtube-collector@youtube-collector-a.service)
stops_po_before_collector() {
  local po_stop_line collector_stop_line
  po_stop_line="$(grep -nFx 'stop hololive-youtube-po.socket' "$1/calls" | head -1 | cut -d: -f1)"
  collector_stop_line="$(grep -nFx 'disable --now hololive-youtube-collector@youtube-collector-a.service' "$1/calls" | head -1 | cut -d: -f1)"
  [[ -n "${po_stop_line}" && -n "${collector_stop_line}" ]] && (( po_stop_line < collector_stop_line ))
}

running_state="$(stop_case running "${running_units[@]}")"
if run_native_stop "${running_state}" >/dev/null &&
   [[ -z "$(ls -A "${running_state}/active")" ]] &&
   stops_po_before_collector "${running_state}"; then
  pass "ap-host-native cutover stops the PO issuer before the collector can retire its generation"
else
  record_fail "ap-host-native cutover must stop the PO issuer before the collector"
fi

cutover_state="$(stop_case cutover "${running_units[@]}")"
run_native_case "${cutover_state}" "set -Eeuo pipefail; ${cutover_stop_step}"
if [[ -n "${cutover_stop_step}" ]] && (( native_rc == 0 )) &&
   [[ -z "$(ls -A "${cutover_state}/active")" ]] &&
   stops_po_before_collector "${cutover_state}"; then
  pass "ap-host-native cutover top-level step stops the PO issuer before the collector"
else
  record_fail "ap-host-native cutover top-level step must stop the PO issuer before the collector"
fi

first_install_state="$(stop_case first-install hololive-youtube-collector@youtube-collector-a.service)"
if run_native_stop "${first_install_state}" >/dev/null &&
   [[ -z "$(ls -A "${first_install_state}/active")" ]] &&
   ! grep -q '^stop ' "${first_install_state}/calls"; then
  pass "ap-host-native cutover stops the collector when no PO issuer is installed"
else
  record_fail "ap-host-native cutover must tolerate absent PO units and still stop the collector"
fi

stuck_state="$(stop_case stuck "${running_units[@]}")"
touch "${stuck_state}/collector_sticks"
if run_native_stop "${stuck_state}" >/dev/null 2>&1; then
  record_fail "ap-host-native cutover must fail while the collector stays active"
else
  pass "ap-host-native cutover fails closed while the collector stays active"
fi

# 실패한 cutover는 ERR trap으로 복원하고 원래 실패 상태(여기서는 false의 1)로 끝난다.
failed_cutover='set -Eeuo pipefail; arm_native_cutover_restore; false'
restore_warning='could not be restored'
restore_state="$(stop_case restore "${running_units[@]}")"
run_native_case "${restore_state}" "${failed_cutover}"
if (( native_rc == 1 )) &&
   ! grep -qF "${restore_warning}" "${restore_state}/stderr" &&
   [[ -z "$(ls -A "${restore_state}/active")" ]] &&
   stops_po_before_collector "${restore_state}" &&
   grep -qFx 'enable --now hololive-youtube-collector@youtube-collector-a.service' "${restore_state}/calls"; then
  pass "ap-host-native failed-cutover restore stops the PO issuer before the collector"
else
  record_fail "ap-host-native failed-cutover restore must stop the PO issuer before the collector and restore the previous release"
fi

restore_fail_state="$(stop_case restore-fails "${running_units[@]}")"
touch "${restore_fail_state}/po_restore_fails"
run_native_case "${restore_fail_state}" "${failed_cutover}"
if (( native_rc == 1 )) &&
   grep -qF "${restore_warning}" "${restore_fail_state}/stderr" &&
   ! grep -qF 'enable --now' "${restore_fail_state}/calls"; then
  pass "ap-host-native failed-cutover restore stops at the first failed step and reports it"
else
  record_fail "ap-host-native failed-cutover restore must stop at a failed step, warn, and keep the cutover status"
fi

# set -E로 ERR trap이 명령 치환에도 상속된다. 치환 안의 실패도 복원은 cutover shell에서 한 번만 하고 원래 상태로 끝난다.
substitution_state="$(stop_case restore-substitution "${running_units[@]}")"
run_native_case "${substitution_state}" 'set -Eeuo pipefail; arm_native_cutover_restore; ready="$(false)"; printf "%s\n" "${ready}"'
if (( native_rc == 1 )) &&
   ! grep -qF "${restore_warning}" "${substitution_state}/stderr" &&
   [[ "$(grep -cFx 'enable --now hololive-youtube-collector@youtube-collector-a.service' "${substitution_state}/calls")" == 1 ]] &&
   [[ "$(grep -cFx 'po_restore_previous '"${substitution_state}/old-release" "${substitution_state}/calls")" == 1 ]]; then
  pass "ap-host-native failed-cutover restore runs once when a command substitution fails"
else
  record_fail "ap-host-native failed-cutover restore must run exactly once when a command substitution fails"
fi

tmp="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp}" "${stop_fixture}"
}
trap cleanup EXIT
mkdir -p "${tmp}/bin" "${tmp}/success" "${tmp}/failure"
touch "${tmp}/KR.key"

if RELEASE_ID='../active' \
   ARTIFACT_DIR="${tmp}/traversal-artifact" \
   SSH_KEY="${tmp}/KR.key" \
   "${DEPLOY}" osaka --dry-run >"${tmp}/traversal.out" 2>"${tmp}/traversal.err"; then
  record_fail "ap-host-native deploy must reject RELEASE_ID traversal before building"
elif [[ -e "${tmp}/traversal-artifact" ]]; then
  record_fail "invalid RELEASE_ID must not create the artifact directory"
elif grep -Fq 'RELEASE_ID must be one safe path component' "${tmp}/traversal.err"; then
  pass "ap-host-native deploy rejects RELEASE_ID traversal before artifact creation"
else
  record_fail "invalid RELEASE_ID must report the safe component contract"
fi

release_root="${tmp}/releases"
mkdir -p "${release_root}/active" "${tmp}/outside"
ln -s "${release_root}/active" "${tmp}/current"
ln -s "${release_root}/active" "${release_root}/active-alias"
ln -s "${tmp}/outside" "${release_root}/escape-alias"

if [[ "$(native_release_dir_resolve "${release_root}" safe-release "${tmp}/current")" == "${release_root}/safe-release" ]]; then
  pass "host-native release resolver accepts a contained inactive release"
else
  record_fail "host-native release resolver must accept a contained inactive release"
fi
if native_release_dir_resolve "${release_root}" active-alias "${tmp}/current" >/dev/null 2>&1; then
  record_fail "host-native release resolver must reject a canonical alias of the active release"
else
  pass "host-native release resolver rejects a canonical alias of the active release"
fi
if native_release_dir_resolve "${release_root}" escape-alias "${tmp}/current" >/dev/null 2>&1; then
  record_fail "host-native release resolver must reject a canonical path outside releases root"
else
  pass "host-native release resolver rejects canonical containment escape"
fi

mkdir -p "${release_root}/previous-release" "${release_root}/inactive-release"
printf 'rollback payload\n' > "${release_root}/previous-release/marker"
ln -s "${release_root}/previous-release" "${tmp}/previous"
ln -s "${release_root}/previous-release" "${release_root}/previous-alias"
ln -s "${release_root}/missing-target" "${release_root}/dangling-release"
printf 'existing artifact\n' > "${release_root}/existing-file"
for existing_release in previous-release inactive-release previous-alias dangling-release existing-file; do
  if native_release_dir_resolve "${release_root}" "${existing_release}" "${tmp}/current" >"${tmp}/existing.out" 2>"${tmp}/existing.err"; then
    record_fail "host-native release resolver must reject existing release path: ${existing_release}"
  else
    pass "host-native release resolver rejects existing release path: ${existing_release}"
  fi
done
if [[ "$(cat "${release_root}/previous-release/marker")" == 'rollback payload' ]] &&
   [[ "$(readlink "${tmp}/previous")" == "${release_root}/previous-release" ]] &&
   [[ "$(readlink "${tmp}/current")" == "${release_root}/active" ]]; then
  pass "rejected native release reuse preserves current and rollback artifacts"
else
  record_fail "rejected native release reuse must preserve current and rollback artifacts"
fi

mkdir -p "${tmp}/rollback-bin" "${tmp}/rollback-fixture/bin" \
  "${tmp}/rollback-fixture/rollback-contract"
cat > "${tmp}/rollback-bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" != "-n" ]] || shift
exec "$@"
EOF
cat > "${tmp}/rollback-bin/systemd-analyze" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
unit="${2:?unit}"
if grep -Fq INVALID_UNIT "${unit}"; then
  exit 1
fi
EOF
chmod +x "${tmp}/rollback-bin/sudo" "${tmp}/rollback-bin/systemd-analyze"

rollback_fixture="${tmp}/rollback-fixture"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/youtube-collector"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/youtube-collector-wrapper"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/healthcheck"
mkdir -p "${rollback_fixture}/youtubejs/src"
printf 'export {}\n' > "${rollback_fixture}/youtubejs/src/server.mjs"
chmod +x "${rollback_fixture}/bin/youtube-collector" \
  "${rollback_fixture}/bin/youtube-collector-wrapper" \
  "${rollback_fixture}/bin/healthcheck"
printf 'APP_ENV=production\n' > "${rollback_fixture}/rollback-contract/youtube-collector-host.env"
printf '[Unit]\nDescription=fixture\n' > "${rollback_fixture}/rollback-contract/hololive-youtube-collector@.service"
printf 'absent\n' > "${rollback_fixture}/rollback-contract/previous-before-cutover"
printf 'absent\n' > "${rollback_fixture}/rollback-contract/po-unit-presence"
(
  cd "${rollback_fixture}"
  sha256sum \
    bin/youtube-collector \
    bin/youtube-collector-wrapper \
    bin/healthcheck \
    rollback-contract/youtube-collector-host.env \
    rollback-contract/hololive-youtube-collector@.service \
    rollback-contract/previous-before-cutover \
    rollback-contract/po-unit-presence \
    > rollback-contract/SHA256SUMS
)

if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}"; then
  pass "native rollback validation accepts a complete integrity fixture"
else
  record_fail "native rollback validation must accept a complete integrity fixture"
fi

mv "${rollback_fixture}/bin/healthcheck" "${rollback_fixture}/bin/healthcheck.missing"
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/missing.out" 2>"${tmp}/missing.err"; then
  record_fail "native rollback validation must reject a missing healthcheck"
elif grep -Fq 'previous host-native executable is missing or not executable: bin/healthcheck' "${tmp}/missing.err"; then
  pass "native rollback validation rejects a missing healthcheck"
else
  record_fail "missing healthcheck validation must fail for the executable precondition"
fi
mv "${rollback_fixture}/bin/healthcheck.missing" "${rollback_fixture}/bin/healthcheck"

printf 'corrupt\n' >> "${rollback_fixture}/bin/youtube-collector"
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/corrupt.out" 2>"${tmp}/corrupt.err"; then
  record_fail "native rollback validation must reject a corrupt binary"
elif grep -Fq 'previous host-native rollback payload failed checksum validation' "${tmp}/corrupt.err"; then
  pass "native rollback validation rejects a corrupt binary"
else
  record_fail "corrupt binary validation must fail for the checksum precondition"
fi
sed -i '$d' "${rollback_fixture}/bin/youtube-collector"
chmod +x "${rollback_fixture}/bin/youtube-collector"

printf 'INVALID_UNIT\n' >> "${rollback_fixture}/rollback-contract/hololive-youtube-collector@.service"
(
  cd "${rollback_fixture}"
  sha256sum \
    bin/youtube-collector \
    bin/youtube-collector-wrapper \
    bin/healthcheck \
    rollback-contract/youtube-collector-host.env \
    rollback-contract/hololive-youtube-collector@.service \
    rollback-contract/previous-before-cutover \
    rollback-contract/po-unit-presence \
    > rollback-contract/SHA256SUMS
)
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/invalid-unit.out" 2>"${tmp}/invalid-unit.err"; then
  record_fail "native rollback validation must reject an invalid systemd unit"
elif grep -Fq 'previous host-native systemd unit failed validation' "${tmp}/invalid-unit.err"; then
  pass "native rollback validation rejects an invalid systemd unit"
else
  record_fail "invalid unit validation must fail for the systemd precondition"
fi

cat > "${tmp}/bin/ssh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
capture_dir="${AP_NATIVE_ROLLBACK_CAPTURE:?}"
counter="${capture_dir}/counter"
call=0
[[ ! -r "${counter}" ]] || call="$(<"${counter}")"
call=$((call + 1))
printf '%s\n' "${call}" > "${counter}"
printf '%s\n' "${!#}" > "${capture_dir}/call-${call}.cmd"
payload="$(cat)"
printf '%s\n' "${payload}" > "${capture_dir}/call-${call}.stdin"

if grep -Fq 'date -u +%Y-%m-%dT%H:%M:%SZ' <<<"${payload}"; then
  printf '2026-08-01T03:04:05Z\n'
fi
if [[ "${AP_NATIVE_ROLLBACK_FAIL_COMPLETION:-false}" == "true" ]] &&
   grep -Fq "collector AP completion check passed" <<<"${payload}"; then
  exit 77
fi
EOF
chmod +x "${tmp}/bin/ssh"

if PATH="${tmp}/bin:${PATH}" \
   SSH_KEY="${tmp}/KR.key" \
   AP_NATIVE_ROLLBACK_CAPTURE="${tmp}/success" \
   I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true \
   "${ROLLBACK}" osaka --apply >"${tmp}/success.out" 2>"${tmp}/success.err"; then
  pass "ap-host-native rollback completes only after the shared completion gate"
else
  cat "${tmp}/success.out"
  cat "${tmp}/success.err" >&2
  record_fail "ap-host-native rollback orchestration must succeed when restore and completion checks pass"
fi

# 두 번째 SSH 호출은 실제 복원 payload입니다.
restore_payload="${tmp}/success/call-2.stdin"

# 수동 rollback이 원격에 보낸 payload에서 검증 뒤 첫 복원 변경 전까지의 정지 단계를 같은 가짜 systemctl로 실행한다.
rollback_stop_step="$(awk '/^native_rollback_validate "[$]previous_target"$/ { on = 1; next } on && /^sudo -n install / { exit } on' "${restore_payload}")"
rollback_state="$(stop_case rollback "${running_units[@]}")"
run_native_case "${rollback_state}" "set -euo pipefail; ${rollback_stop_step}"
if [[ -n "${rollback_stop_step}" ]] && (( native_rc == 0 )) &&
   [[ -z "$(ls -A "${rollback_state}/active")" ]] &&
   stops_po_before_collector "${rollback_state}"; then
  pass "ap-host-native rollback stops the PO issuer before the collector"
else
  record_fail "ap-host-native rollback must stop the PO issuer before the collector"
fi
rm -rf "${stop_fixture}"


if PATH="${tmp}/bin:${PATH}" \
   SSH_KEY="${tmp}/KR.key" \
   AP_NATIVE_ROLLBACK_CAPTURE="${tmp}/failure" \
   AP_NATIVE_ROLLBACK_FAIL_COMPLETION=true \
   I_APPROVE_OSAKA_ACTIVE_ACTIVE_ROLLBACK=true \
   "${ROLLBACK}" osaka --apply >"${tmp}/failure.out" 2>"${tmp}/failure.err"; then
  record_fail "ap-host-native rollback must fail when the completion gate fails"
else
  pass "ap-host-native rollback propagates completion gate failure"
fi

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all ap-host-native release checks passed"
