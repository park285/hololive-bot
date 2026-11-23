#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="${ROOT_DIR}/scripts/deploy/ap-host-native-deploy.sh"
REMOTE_APPLY="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-remote-apply.sh"
ROLLBACK="${ROOT_DIR}/scripts/deploy/ap-host-native-rollback.sh"
RELEASE_PATH_LIB="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-release-path.sh"
ROLLBACK_CHECK_LIB="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-rollback-check.sh"
UNIT_TEMPLATE="${ROOT_DIR}/scripts/deploy/lib/hololive-collector.service"

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

# shellcheck source=scripts/deploy/lib/ap-host-native-release-path.sh
. "${RELEASE_PATH_LIB}"
# shellcheck source=scripts/deploy/lib/ap-host-native-rollback-check.sh
. "${ROLLBACK_CHECK_LIB}"
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
. "${ROOT_DIR}/scripts/deploy/lib/postgres-endpoint.sh"
eval "$(. "${ROOT_DIR}/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble "$(collector_slots_where deploy native)")"
native_host_env="${slot_host_env##*/}" native_unit_template="${slot_collector_unit%%@*}@.service"
po_service="${slot_issuer_unit?}" po_socket="${slot_issuer_unit%.service}.socket"


write_host_env_fn="$(awk '/^write_host_env\(\) \{/,/^}$/' "${DEPLOY}")"
cfg008_dir="$(mktemp -d)"
generated_env="${cfg008_dir}/collector-host.env"
if [[ -z "${write_host_env_fn}" ]]; then
  record_fail "ap-host-native write_host_env function is missing"
elif (
  # shellcheck disable=SC2178
  export service="collector-a"
  export port="30005"
  export release_id="profile-pair-fixture"
  export REPO_ROOT="${ROOT_DIR}"
  export AP_POSTGRES_PORT=5433
  eval "$(awk '/^AP_POSTGRES_HOST=""/,/^\[\[ -n "\$AP_POSTGRES_HOST" \]\]/' "${DEPLOY}")"
  eval "$(awk '/^AP_POSTGRES_POOL_MAX_CONNS=""/,/^\[\[ "\$AP_POSTGRES_POOL_MAX_CONNS"/' "${DEPLOY}")"
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
    record_fail "generated host env must use the central PostgreSQL TLS endpoint"
  else
    pass "generated host env contents have 0 CACHE lines"
    pass "generated host env contents have 0 SETTINGS_DIR lines"
    pass "generated host env emits the dedicated internal H3 client keys"
  fi
else
  record_fail "ap-host-native write_host_env did not produce generated env contents"
fi
if direct_error="$(
  hostname() { echo kapu; }
  export -f hostname
  AP_POSTGRES_PORT=6433 "$DEPLOY" osaka --dry-run 2>&1
)"; then
  record_fail "ap-host-native deploy accepted the retired pooler port"
elif [[ "$direct_error" == 'AP_POSTGRES_PORT must be the central PostgreSQL TLS port (5433)' ]]; then
  pass "ap-host-native deploy rejects the retired pooler port before any build or transport"
else
  record_fail "ap-host-native deploy did not reject the retired pooler port at entry"
fi

(
  set -euo pipefail
  fixture="$(mktemp -d)"
  trap 'rm -rf "$fixture"' EXIT
  tool="$fixture/tool" product="$fixture/product"
  mkdir -p "$tool/scripts/deploy/lib" "$product"/{deploy/topology,scripts/ci,scripts/deploy/{ap-hosts,lib}}
  cp "$DEPLOY" "$ROOT_DIR/scripts/deploy/verify-exec-tree-ownership.sh" "$tool/scripts/deploy/"
  cp "$ROOT_DIR"/scripts/deploy/lib/{ap-host.sh,ap-host-native-*.sh,source-revision.sh,compose-env.sh,postgres-endpoint.sh,youtubejs-node-version.sh,ap-collector-readiness.sh,collector-slots.sh,collector-slots.tsv} "$tool/scripts/deploy/lib/"
  printf '\n: fixture-tool-window\n' >>"$tool/scripts/deploy/lib/ap-host-native-window.sh"
  grep '^native_release_window=' "$tool/scripts/deploy/lib/ap-host-native-window.sh" >"$product/scripts/deploy/lib/ap-host-native-window.sh"
  printf ': fixture-product-window\n' >>"$product/scripts/deploy/lib/ap-host-native-window.sh"
  cp "$ROOT_DIR/deploy/topology/hosts.conf" "$product/deploy/topology/"
  printf '%s\n' "$write_host_env_fn" >"$product/scripts/deploy/ap-host-native-deploy.sh"
  cp "$ROOT_DIR"/scripts/deploy/lib/{hololive-collector.service,hololive-youtube-po.service,hololive-youtube-po.socket} "$product/scripts/deploy/lib/"
  cp "$ROOT_DIR/scripts/ci/postgres-capacity-policy.tsv" "$product/scripts/ci/"
  cp "$ROOT_DIR/scripts/deploy/lib/collector-slots.tsv" "$product/scripts/deploy/lib/"
  sed -e 's/^AP_GOAMD64=.*/AP_GOAMD64=v1/' -e 's/^AP_SSH_HOST=.*/AP_SSH_HOST=fixture-ap/' \
    "$ROOT_DIR/scripts/deploy/ap-hosts/osaka.conf" >"$product/scripts/deploy/ap-hosts/osaka.conf"
  for checkout in "$tool" "$product"; do
    git -C "$checkout" init -q
    git -C "$checkout" add .
    git -C "$checkout" -c user.name=fixture -c user.email=fixture@example.invalid commit -qm fixture
  done
  ln -s "$tool/scripts/deploy/ap-host-native-deploy.sh" "$fixture/deploy"
  SSH_KEY="$fixture/fixture.key"
  touch "$SSH_KEY"
  AP_NATIVE_TOOL_REVISION="$(git -C "$tool" rev-parse HEAD)"
  AP_NATIVE_PRODUCT_REVISION="$(git -C "$product" rev-parse HEAD)"
  export SSH_KEY AP_NATIVE_TOOL_REVISION AP_NATIVE_PRODUCT_REVISION fixture
  export REPO_ROOT="$product" I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true
  unset RELEASE_ID
  hostname() { echo kapu; }
  ssh() {
    printf '%s\n' "$@" >"$fixture/request"
    cat >"$fixture/bundle"
    return 77
  }
  export -f hostname ssh
  rc=0
  "$fixture/deploy" osaka --apply 2>"$fixture/result" || rc=$?
  [[ "$rc" == 77 ]]
  grep -Fxq "native deploy source: tool=$AP_NATIVE_TOOL_REVISION product=$AP_NATIVE_PRODUCT_REVISION" "$fixture/result"
  grep -Fxq 'ubuntu@fixture-ap' "$fixture/request"
  grep -Fq -- "-${AP_NATIVE_PRODUCT_REVISION:0:12}-osaka" "$fixture/request"
  request="$(tail -n1 "$fixture/request")"
  read -r _ _ _ _ requested_id _ <<<"$request"
  env_hash="$(sed -e "s/profile-pair-fixture/$requested_id/g" -e 's/100\.100\.1\.6:30096/fixture-ap:30096/' "$generated_env" | sha256sum)"
  collector_hash="$(sha256sum "$product/scripts/deploy/lib/hololive-collector.service")"
  issuer_hash="$(sed "s/@RELEASE_ID@/$requested_id/g" "$product/scripts/deploy/lib/hololive-youtube-po.service" | sha256sum)"
  socket_hash="$(sha256sum "$product/scripts/deploy/lib/hololive-youtube-po.socket")"
  [[ "$request" == *" $AP_NATIVE_PRODUCT_REVISION apply-origin ${env_hash%% *} ${collector_hash%% *} ${issuer_hash%% *} ${socket_hash%% *}" ]]
  grep -Fxq ': fixture-tool-window' "$fixture/bundle"
  if grep -Fxq ': fixture-product-window' "$fixture/bundle"; then exit 1; fi
  rm "$fixture/request" "$fixture/bundle"
  for pin in AP_NATIVE_TOOL_REVISION AP_NATIVE_PRODUCT_REVISION; do
    if env "$pin=0000000000000000000000000000000000000000" "$fixture/deploy" osaka --apply 2>"$fixture/result"; then exit 1; fi
    grep -Fq 'two approved full revisions' "$fixture/result"
    [[ ! -e "$fixture/request" ]]
  done
  for checkout in "$tool" "$product"; do
    touch "$checkout/unreviewed"
    if "$fixture/deploy" osaka --apply 2>"$fixture/result"; then exit 1; fi
    grep -Fq 'worktree is not clean' "$fixture/result"
    [[ ! -e "$fixture/request" ]]
    rm "$checkout/unreviewed"
  done
  for checkout in "$tool" "$product"; do
    printf '/artifacts/\n' >>"$checkout/.git/info/exclude"
    nested="$checkout/artifacts/pseudo"
    mkdir -p "$nested"
    cp -R "$checkout/scripts" "$nested/"
    if [[ "$checkout" == "$product" ]]; then
      cp -R "$checkout/deploy" "$nested/"
      entry="$fixture/deploy" src="$nested"
    else
      entry="$nested/scripts/deploy/ap-host-native-deploy.sh" src="$product"
    fi
    if REPO_ROOT="$src" "$entry" osaka --apply 2>"$fixture/result"; then exit 1; fi
    grep -Fq 'checkout top-level' "$fixture/result"
    [[ ! -e "$fixture/request" ]]
  done
  sed -i 's/APP_ENV=production/APP_ENV=fixture/' "$product/scripts/deploy/ap-host-native-deploy.sh"
  git -C "$product" -c user.name=fixture -c user.email=fixture@example.invalid commit -qam 'different env generator'
  AP_NATIVE_PRODUCT_REVISION="$(git -C "$product" rev-parse HEAD)"
  if "$fixture/deploy" osaka --apply 2>"$fixture/result"; then exit 1; fi
  grep -Fq 'template differs from the approved product' "$fixture/result"
  [[ ! -e "$fixture/request" ]]
  printf '%s\n' "$write_host_env_fn" >"$product/scripts/deploy/ap-host-native-deploy.sh"
  printf 'native_release_window=W2\n' >"$product/scripts/deploy/lib/ap-host-native-window.sh"
  git -C "$product" -c user.name=fixture -c user.email=fixture@example.invalid commit -qam 'different family'
  AP_NATIVE_PRODUCT_REVISION="$(git -C "$product" rev-parse HEAD)"
  if "$fixture/deploy" osaka --apply 2>"$fixture/result"; then exit 1; fi
  grep -Fq 'native tool/product release window mismatch' "$fixture/result"
  [[ ! -e "$fixture/request" ]]
)
pass "native tool and product revisions are independent and checked before transport"
(
  set -euo pipefail
  fixture="$(mktemp -d)"
  trap 'rm -rf "$fixture"' EXIT
  tool="$fixture/tool" product="$fixture/product"
  mkdir -p "$tool/scripts/deploy/lib" "$tool/scripts/logs" \
    "$product"/{deploy/topology,scripts/build,scripts/ci,scripts/deploy/{ap-hosts,lib},hololive/hololive-collector/youtubejs/src}
  cp "$DEPLOY" "$ROOT_DIR/scripts/deploy/verify-exec-tree-ownership.sh" "$tool/scripts/deploy/"
  cp "$ROOT_DIR"/scripts/deploy/lib/{ap-host.sh,ap-host-native-*.sh,source-revision.sh,compose-env.sh,postgres-endpoint.sh,youtubejs-node-version.sh,ap-collector-readiness.sh,collector-slots.sh,collector-slots.tsv} "$tool/scripts/deploy/lib/"
  cp "$ROOT_DIR"/scripts/deploy/lib/{ap-host-native-*.sh,compose-env.sh,postgres-endpoint.sh,worker-profile-pair.py,hololive-collector.service,hololive-youtube-po.service,hololive-youtube-po.socket,collector-slots.tsv} "$product/scripts/deploy/lib/"
  cp "$DEPLOY" "$product/scripts/deploy/"
  cp "$ROOT_DIR/deploy/topology/hosts.conf" "$product/deploy/topology/"
  cp "$ROOT_DIR/scripts/ci/postgres-capacity-policy.tsv" "$product/scripts/ci/"
  sed -e 's/^AP_GOAMD64=.*/AP_GOAMD64=v1/' -e 's/^AP_SSH_HOST=.*/AP_SSH_HOST=fixture-ap/' \
    "$ROOT_DIR/scripts/deploy/ap-hosts/osaka.conf" >"$product/scripts/deploy/ap-hosts/osaka.conf"
  for observer in scripts/logs/ap-host-native-status.sh scripts/deploy/ap-completion-check.sh; do
    printf '#!/usr/bin/env bash\nprintf "observer %%s\\n" "${0##*/}" >>"$OBSERVED"\n[[ ! -e "$OBSERVED.fail" ]]\n' >"$tool/$observer"
    chmod +x "$tool/$observer"
  done
  printf '#!/bin/sh\nmkdir -p "$2/bin"\n: >"$2/bin/hololive-collector"\n' >"$product/scripts/build/build-hololive-collector-go.sh"
  printf '#!/bin/sh\n' >"$product/scripts/build/check-hololive-collector-go-artifact.sh"
  printf '#!/bin/sh\nmkdir -p "$4" && cd "$4" && touch rootfs.tar rootfs.tar.sha256 rootfs-manifest.json revision architecture version\n' \
    >"$product/scripts/build/build-po-sandbox-artifact.sh"
  chmod +x "$product/scripts/build/build-po-sandbox-artifact.sh"
  touch "$product/scripts/build/po-sandbox-manifest.py" "$product"/hololive/hololive-collector/youtubejs/{package.json,package-lock.json,src/server.mjs}
  for checkout in "$tool" "$product"; do
    git -C "$checkout" init -q
    git -C "$checkout" add .
    git -C "$checkout" -c user.name=fixture -c user.email=fixture@example.invalid commit -qm fixture
  done
  ln -s "$tool/scripts/deploy/ap-host-native-deploy.sh" "$fixture/deploy"
  SSH_KEY="$fixture/fixture.key" OBSERVED="$fixture/observed"
  touch "$SSH_KEY"
  AP_NATIVE_TOOL_REVISION="$(git -C "$tool" rev-parse HEAD)"
  AP_NATIVE_PRODUCT_REVISION="$(git -C "$product" rev-parse HEAD)"
  export SSH_KEY OBSERVED AP_NATIVE_TOOL_REVISION AP_NATIVE_PRODUCT_REVISION REPO_ROOT="$product" I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true
  unset RELEASE_ID
  hostname() { echo kapu; }
  ssh() {
    local request="${!#}"
    cat >/dev/null
    printf '%s\n' "$request" >>"$OBSERVED"
    if [[ "$request" == 'bash -s --' ]]; then echo 2026-11-22T00:00:00Z; fi
  }
  rsync() { :; }
  npm() { :; }
  export -f hostname ssh rsync npm
  for outcome in accepted observer-failed; do
    rm -f "$OBSERVED" "$OBSERVED.fail"
    if [[ "$outcome" != accepted ]]; then touch "$OBSERVED.fail"; fi
    status=0
    ARTIFACT_DIR="$fixture/artifact-$outcome" "$fixture/deploy" osaka --install-initial 2>"$fixture/$outcome.err" || status=$?
    steps="$(sed -nE -e "s/^bash -s -- [^ ]+ [^ ]+ collector-a 30005 [^ ]+ [0-9]+ [0-9]+ $AP_NATIVE_PRODUCT_REVISION (install-initial|initial-accepted)$/\1/p" \
      -e 's/^observer (.*)$/\1/p' "$OBSERVED" | paste -sd' ')"
    if [[ "$outcome" == accepted ]]; then
      [[ "$status" == 0 && "$steps" == 'install-initial ap-host-native-status.sh ap-completion-check.sh initial-accepted' ]] ||
        { cat "$fixture/$outcome.err" >&2; printf '%s\n' "$steps" >&2; exit 1; }
    else
      [[ "$status" != 0 && "$steps" == 'install-initial ap-host-native-status.sh' ]] ||
        { cat "$fixture/$outcome.err" >&2; printf '%s\n' "$steps" >&2; exit 1; }
    fi
  done
) && pass 'osaka 첫 설치는 관찰 검사 두 개가 통과한 뒤에만 수용을 요청합니다.' ||
  record_fail 'osaka 첫 설치의 설치·관찰·수용 순서가 맞지 않습니다.'
rm -rf "${cfg008_dir}"

permission_fn="$(awk '/^normalize_runtime_payload_permissions\(\) \{/,/^}$/' "${REMOTE_APPLY}")"
permission_fixture="$(mktemp -d)"
if [[ -z "${permission_fn}" ]]; then
  record_fail "host-native payload permission normalizer is missing"
elif (
  sudo() {
    [[ "${1:-}" != "-n" ]] || shift
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


unit_copy_dir="$(mktemp -d)"
mkdir -p "${unit_copy_dir}/etc/systemd/system" \
  "${unit_copy_dir}/opt/hololive-bot/collector/current/bin"
cp "${UNIT_TEMPLATE}" "${unit_copy_dir}/etc/systemd/system/${native_unit_template}"
printf '#!/bin/sh\nexit 0\n' >"${unit_copy_dir}/opt/hololive-bot/collector/current/bin/hololive-collector-wrapper"
chmod +x "${unit_copy_dir}/opt/hololive-bot/collector/current/bin/hololive-collector-wrapper"
for fixture_target in sysinit.target basic.target network-online.target shutdown.target sockets.target paths.target; do
  printf '[Unit]\nDescription=fixture\n' >"${unit_copy_dir}/etc/systemd/system/${fixture_target}"
done
if systemd-analyze verify --root="${unit_copy_dir}" "/etc/systemd/system/${native_unit_template}"; then
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
  -p Type=exec -p RuntimeMaxSec=300s -p KillMode=control-group \
  -p "WorkingDirectory=${ROOT_DIR}" \
  bash "${ROOT_DIR}/scripts/deploy/ap-host-native-cutover_test.sh"

native_units_fns="$(awk '/^stop_native_units_and_require_inactive\(\) \{/,/^}$/; /^native_restore_recorded_runtime\(\) \{/,/^}$/; /^restore_native_after_failed_cutover\(\) \{/,/^}$/; /^native_cutover_failed\(\) \{/,/^}$/; /^arm_native_cutover_restore\(\) \{/,/^}$/' "${REMOTE_APPLY}")"
cutover_stop_step='arm_native_cutover_restore; native_cutover_run stop stop_native_units_and_require_inactive'
stop_fixture="$(mktemp -d)"
trap 'rm -rf "${stop_fixture}"' EXIT
run_native_units() (
  local state="$1" script="$2"
  unit="$slot_collector_unit"
  # shellcheck disable=SC2034 # eval한 실제 복원 함수가 읽는 변경 전 실행 상태입니다.
  collector_prechange_running=true collector_prechange_enabled=true
  old_target="${state}/old-release"
  # shellcheck disable=SC2034 # eval한 실제 복원 함수가 읽는 경로다.
  host_env="${state}/host.env" unit_file="${state}/unit" current_link="${state}/current"
  # shellcheck disable=SC2034 # eval한 실제 복원 함수가 읽는 경로입니다.
  releases_root="${state}/releases" previous_link="${state}/previous"
  mkdir -p "${old_target}"
  release_dir="${state}/new-release"
  mkdir -p "${old_target}/rollback-contract" "${release_dir}"
  printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=5433\n' > "${release_dir}/${native_host_env}"
  cp "${release_dir}/${native_host_env}" "${old_target}/rollback-contract/${native_host_env}"
  cp "${release_dir}/${native_host_env}" "${host_env}"
  native_cutover_run() {
    shift
    # shellcheck disable=SC2034 # eval한 실제 복원 함수가 읽는 완료 상태입니다.
    native_completion_verified=true
    ( set -e; "$@"; )
  }
  native_cutover_release_guard() { :; }
  native_rollback_validate() { [[ -d "$1" ]]; }
  sudo() {
    [[ "${1:-}" != "-n" ]] || shift
    if [[ "$1" == systemctl || "$1" == bash ]]; then
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
      show)
        case "$4" in
          LoadState)
            if [[ -e "${state}/installed/$2" ]]; then echo loaded; else echo not-found; fi ;;
          ActiveState)
            if [[ -e "${state}/active/$2" ]]; then echo active; else echo inactive; fi ;;
          UnitFileState)
            if [[ -e "${state}/enabled/$2" ]]; then echo enabled; else echo disabled; fi ;;
          *) return 1 ;;
        esac
        ;;
      stop | disable)
        [[ "$1" == stop ]] || shift
        shift
        for target in "$@"; do
          if [[ ! -e "${state}/stuck-enable-${target}" ]]; then rm -f "${state}/enabled/${target}"; fi
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
run_native_case() {
  set +e
  run_native_units "$1" "$2" >"$1/stdout" 2>"$1/stderr"
  native_rc=$?
  set -e
}
stop_case() {
  local state="${stop_fixture}/$1" unit_name
  shift
  mkdir -p "${state}/active" "${state}/enabled" "${state}/installed"
  for unit_name in "$@"; do
    touch "${state}/active/${unit_name}" "${state}/enabled/${unit_name}" "${state}/installed/${unit_name}"
  done
  printf '%s\n' "${state}"
}
running_units=("$po_socket" "$po_service" "$slot_collector_unit")
stops_po_before_collector() {
  local po_stop_line collector_stop_line
  po_stop_line="$(grep -nEx '(stop|disable --now) hololive-youtube-po\.socket' "$1/calls" | head -1 | cut -d: -f1)"
  collector_stop_line="$(grep -nFx "disable --now $slot_collector_unit" "$1/calls" | head -1 | cut -d: -f1)"
  [[ -n "${po_stop_line}" && -n "${collector_stop_line}" ]] && (( po_stop_line < collector_stop_line ))
}

running_state="$(stop_case running "${running_units[@]}")"
if run_native_stop "${running_state}" >/dev/null &&
   [[ -z "$(ls -A "${running_state}/active")" ]] &&
   [[ -z "$(ls -A "${running_state}/enabled")" ]] &&
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

first_install_state="$(stop_case first-install "$slot_collector_unit")"
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

for brake_unit in "${running_units[@]}"; do
  stale_enable_state="$(stop_case "enabled-${brake_unit}" "${running_units[@]}")"
  touch "${stale_enable_state}/stuck-enable-${brake_unit}"
  if run_native_stop "${stale_enable_state}" >/dev/null 2>&1; then
    record_fail "ap-host-native cutover accepted reboot-enabled ${brake_unit}"
  else
    pass "ap-host-native cutover rejects reboot-enabled ${brake_unit}"
  fi
done
failed_cutover='set -Eeuo pipefail; arm_native_cutover_restore; false'
restore_warning='could not be restored'
restore_state="$(stop_case restore "${running_units[@]}")"
run_native_case "${restore_state}" "${failed_cutover}"
if (( native_rc == 1 )) &&
   ! grep -qF "${restore_warning}" "${restore_state}/stderr" &&
   [[ -z "$(ls -A "${restore_state}/active")" ]] &&
   stops_po_before_collector "${restore_state}" &&
   grep -qFx "enable --now $slot_collector_unit" "${restore_state}/calls"; then
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

substitution_state="$(stop_case restore-substitution "${running_units[@]}")"
run_native_case "${substitution_state}" 'set -Eeuo pipefail; arm_native_cutover_restore; ready="$(false)"; printf "%s\n" "${ready}"'
if (( native_rc == 1 )) &&
   ! grep -qF "${restore_warning}" "${substitution_state}/stderr" &&
   [[ "$(grep -cFx "enable --now $slot_collector_unit" "${substitution_state}/calls")" == 1 ]] &&
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

printf '#!/usr/bin/env bash\nexit 1\n' > "${tmp}/bin/ssh"
printf '#!/usr/bin/env bash\n[[ "$3" == status ]] && exit 0\nexec %q "$@"\n' "$(command -v git)" > "${tmp}/bin/git"
chmod +x "${tmp}/bin/ssh" "${tmp}/bin/git"

if PATH="${tmp}/bin:${PATH}" \
   RELEASE_ID='../active' \
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

if (
  export NATIVE_TEST_ROOT="$ROOT_DIR" NATIVE_TEST_BASE="$tmp/no-active-release"
  mkdir -p "$NATIVE_TEST_BASE/releases"
  chmod 0700 "$NATIVE_TEST_BASE/releases"
  hostname() { echo kapu; }
  git() { if [[ "$3" == status ]]; then return 0; fi; command git "$@"; }
  ssh() {
    cat >/dev/null
    case "${!#}" in
      *' -- v3') return 0 ;;
      *' apply-origin '*)
        hostname() { echo osaka1; }
        sudo() { shift; "$@"; }
        . "$NATIVE_TEST_ROOT/scripts/deploy/lib/ap-host-native-cutover.sh"
        . "$NATIVE_TEST_ROOT/scripts/deploy/lib/ap-host-native-window.sh"
        current_link="$NATIVE_TEST_BASE/current" releases_root="$NATIVE_TEST_BASE/releases"
        # shellcheck disable=SC2178
        native_recovery_dir="$NATIVE_TEST_BASE/recovery" service=collector-a
        touch "$NATIVE_TEST_BASE/checked"
        native_apply_preflight ;;
      *) return 91 ;;
    esac
  }
  sh() { return 92; }
  rsync() { touch "$NATIVE_TEST_BASE/transferred"; return 93; }
  export -f hostname git ssh sh rsync
  SSH_KEY="$tmp/KR.key" RELEASE_ID=new HOLO_BOT_VERSION=123 ARTIFACT_DIR="$tmp/unadmitted-build" \
    I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true "$DEPLOY" osaka --apply
) >"$tmp/no-current.out" 2>&1 || [[ ! -e "$tmp/no-active-release/checked" || -e "$tmp/unadmitted-build" || -e "$tmp/no-active-release/transferred" ]]; then
  record_fail '오사카 첫 일반 apply가 수용된 원점 없이 빌드·전송 입구를 통과했습니다.'
else
  pass '오사카 첫 일반 apply는 빌드·전송 전에 실제 원점 부재를 확인하고 거절합니다.'
fi

status=0
PATH="$tmp/bin:$PATH" ARTIFACT_DIR="$tmp/initial-other-host" SSH_KEY="$tmp/KR.key" I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true \
  "$DEPLOY" seoul --install-initial >"$tmp/initial-other.out" 2>&1 || status=$?
if [[ "$status" != 2 || -e "$tmp/initial-other-host" ]]; then
  record_fail '첫 설치 모드가 osaka 밖에서 빌드 전에 거절되지 않았습니다.'
else
  pass '첫 설치 모드는 osaka에만 허용됩니다.'
fi
status=0
PATH="$tmp/bin:$PATH" ARTIFACT_DIR="$tmp/initial-unapproved" SSH_KEY="$tmp/KR.key" \
  "$DEPLOY" osaka --install-initial >"$tmp/initial-unapproved.out" 2>&1 || status=$?
if [[ "$status" != 2 || -e "$tmp/initial-unapproved" ]] || ! grep -Fq 'Refusing apply without I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true' "$tmp/initial-unapproved.out"; then
  record_fail '승인 없는 osaka 첫 설치가 빌드 전에 거절되지 않았습니다.'
else
  pass '승인 없는 osaka 첫 설치는 빌드 전에 거절됩니다.'
fi

. "$ROOT_DIR/scripts/deploy/lib/ap-host-native-window.sh"
mkdir -p "$tmp/payload/bin" "$tmp/transfer-home"
printf 'qualified\n' > "$tmp/payload/bin/worker-profile-pair.py"
qualified_digest="$(native_payload_digest "$tmp/payload")"
printf 'different\n' > "$tmp/payload/bin/worker-profile-pair.py"
for mode in --quiesce --resume; do
  args=(osaka "$mode" "$native_release_window" old)
  if [[ "$mode" == --resume ]]; then args+=(12345678-1234-1234-1234-123456789012); fi
  status=0
  error="$(PATH="$tmp/bin:$PATH" RELEASE_ID=new HOLO_BOT_VERSION=123 ARTIFACT_DIR="$tmp/payload" \
    SSH_KEY="$tmp/KR.key" I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true HOLO_NATIVE_PAYLOAD_SHA256="$qualified_digest" \
    "$DEPLOY" "${args[@]}" 2>&1)" || status=$?
  [[ "$status" == 1 && "$error" == *'검증 당시 native payload digest와 다릅니다.' ]] || record_fail "$mode: 검증 후 교체된 helper 허용"
  status=0
  PATH="$tmp/bin:$PATH" HOLO_NATIVE_PAYLOAD_SHA256='' RELEASE_ID=new HOLO_BOT_VERSION=123 "$DEPLOY" "${args[@]}" >/dev/null 2>&1 || status=$?
  [[ "$status" == 2 ]] || record_fail "$mode: 독립 digest 누락 처리 오류"
done

python3 - "$DEPLOY" "$tmp" <<'PY'
from pathlib import Path
import subprocess, sys
entry, temp = map(Path, sys.argv[1:])
body = entry.read_text().split('RSYNC_RSH="$(ap_rsync_rsh)"',1)[1].split('change_started_at=',1)[0]
fixture = '''set -euo pipefail
HOME="$1/transfer-home" payload_name=claimed artifact_dir="$1/payload" MODE="$2" RSYNC_RSH=fixture
ap_remote_bash() { bash -s -- "$@"; }
ap_rsync_target() { printf '%s' "$1"; }
rsync() { printf '%s' "$MODE" > "$HOME/$payload_name/last-transfer"; }
'''
receipt = temp/'transfer-home/claimed/last-transfer'
for mode, expected in [('--quiesce', True), ('--apply', False), ('--resume', True)]:
    result = subprocess.run(['bash','-c',fixture+body,'fixture',str(temp),mode],capture_output=True)
    assert (result.returncode == 0) == expected and receipt.read_text() == '--quiesce', (mode, result.stderr)
PY

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
[[ "${1:-}" != "-u" ]] || shift 2
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
cat > "${tmp}/rollback-bin/systemd-run" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
while [[ "${1:-}" == --* ]]; do shift; done
exec "$@"
EOF
chmod +x "${tmp}/rollback-bin/sudo" "${tmp}/rollback-bin/systemd-analyze" "${tmp}/rollback-bin/systemd-run"

rollback_fixture="${tmp}/rollback-fixture"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/hololive-collector"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/hololive-collector-wrapper"
printf '#!/bin/sh\nexit 0\n' > "${rollback_fixture}/bin/healthcheck"
mkdir -p "${rollback_fixture}/youtubejs/src"
printf 'export {}\n' > "${rollback_fixture}/youtubejs/src/server.mjs"
chmod +x "${rollback_fixture}/bin/hololive-collector" \
  "${rollback_fixture}/bin/hololive-collector-wrapper" \
  "${rollback_fixture}/bin/healthcheck"
printf 'APP_ENV=production\nSTACK_WORKER_PROFILE_FILE=%s/worker-profile.json\n' "$rollback_fixture" > "${rollback_fixture}/rollback-contract/${native_host_env}"
printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=5433\n' >> "${rollback_fixture}/rollback-contract/${native_host_env}"
printf '{}\n' > "$rollback_fixture/worker-profile.json"
(cd "$rollback_fixture" && sha256sum bin/hololive-collector worker-profile.json > SHA256SUMS)
printf '[Unit]\nDescription=fixture\n' > "${rollback_fixture}/rollback-contract/${native_unit_template}"
printf 'absent\n' > "${rollback_fixture}/rollback-contract/previous-before-cutover"
printf 'absent\n' > "${rollback_fixture}/rollback-contract/po-unit-presence"
(
  cd "${rollback_fixture}"
  sha256sum \
    bin/hololive-collector \
    bin/hololive-collector-wrapper \
    bin/healthcheck \
    worker-profile.json SHA256SUMS \
    "rollback-contract/${native_host_env}" \
    "rollback-contract/${native_unit_template}" \
    rollback-contract/previous-before-cutover \
    rollback-contract/po-unit-presence \
    > rollback-contract/SHA256SUMS
)
native_current_env="${tmp}/native-current.env"
for native_port in 5433 6433 invalid ''; do
  printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=%s\n' "${native_port}" > "${native_current_env}"
  if [[ "${native_port}" == 5433 ]]; then
    PATH="${tmp}/rollback-bin:${PATH}" native_rollback_endpoint_validate "${rollback_fixture}" "${native_current_env}" ||
      record_fail "native same-endpoint rollback was rejected"
  elif PATH="${tmp}/rollback-bin:${PATH}" native_rollback_endpoint_validate "${rollback_fixture}" "${native_current_env}" >"${tmp}/endpoint.err" 2>&1; then
    record_fail "native unsafe endpoint rollback accepted"
  fi
done

if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}"; then
  pass "native rollback validation accepts a complete integrity fixture"
else
  record_fail "native rollback validation must accept a complete integrity fixture"
fi

mv "${rollback_fixture}/bin/healthcheck" "${rollback_fixture}/bin/healthcheck.missing"
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/missing.out" 2>"${tmp}/missing.err"; then
  record_fail "native rollback validation must reject a missing healthcheck"
else
  pass "native rollback validation rejects a missing healthcheck"
fi
mv "${rollback_fixture}/bin/healthcheck.missing" "${rollback_fixture}/bin/healthcheck"

printf 'corrupt\n' >> "${rollback_fixture}/bin/hololive-collector"
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/corrupt.out" 2>"${tmp}/corrupt.err"; then
  record_fail "native rollback validation must reject a corrupt binary"
else
  pass "native rollback validation rejects a corrupt binary"
fi
sed -i '$d' "${rollback_fixture}/bin/hololive-collector"
chmod +x "${rollback_fixture}/bin/hololive-collector"

printf 'INVALID_UNIT\n' >> "${rollback_fixture}/rollback-contract/${native_unit_template}"
(
  cd "${rollback_fixture}"
  sha256sum \
    bin/hololive-collector \
    bin/hololive-collector-wrapper \
    bin/healthcheck \
    worker-profile.json SHA256SUMS \
    "rollback-contract/${native_host_env}" \
    "rollback-contract/${native_unit_template}" \
    rollback-contract/previous-before-cutover \
    rollback-contract/po-unit-presence \
    > rollback-contract/SHA256SUMS
)
if PATH="${tmp}/rollback-bin:${PATH}" native_rollback_validate "${rollback_fixture}" >"${tmp}/invalid-unit.out" 2>"${tmp}/invalid-unit.err"; then
  record_fail "native rollback validation must reject an invalid systemd unit"
else
  pass "native rollback validation rejects an invalid systemd unit"
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

restore_payload="${tmp}/success/call-2.stdin"

rollback_stop_step="$(awk '/^native_rollback_endpoint_validate "[$]previous_target" "[$]host_env"$/ { on = 1; next } on && /^sudo -n install / { exit } on' "${restore_payload}")"
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
