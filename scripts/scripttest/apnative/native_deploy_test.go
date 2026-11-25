package apnative

import (
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const nativeDeployPrelude = `tmp="$1"
ROOT_DIR="$SCRIPTTEST_ROOT"
DEPLOY="$ROOT_DIR/scripts/deploy/ap-host-native-deploy.sh"
REMOTE_APPLY="$ROOT_DIR/scripts/deploy/lib/ap-host-native-remote-apply.sh"
ROLLBACK="$ROOT_DIR/scripts/deploy/ap-host-native-rollback.sh"
UNIT_TEMPLATE="$ROOT_DIR/scripts/deploy/lib/hololive-collector.service"
failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { :; }
. "$ROOT_DIR/scripts/deploy/lib/ap-host-native-release-path.sh"
. "$ROOT_DIR/scripts/deploy/lib/ap-host-native-rollback-check.sh"
. "$ROOT_DIR/scripts/deploy/lib/compose-env.sh"
. "$ROOT_DIR/scripts/deploy/lib/postgres-endpoint.sh"
eval "$(. "$ROOT_DIR/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble "$(collector_slots_where deploy native)")"
native_host_env="${slot_host_env##*/}" native_unit_template="${slot_collector_unit%%@*}@.service"
po_service="${slot_issuer_unit?}" po_socket="${slot_issuer_unit%.service}.socket"
write_host_env_fn="$(awk '/^write_host_env\(\) \{/,/^}$/' "${DEPLOY}")"
generated_env="$tmp/collector-host.env"
(
  export service="collector-a" port="30005" release_id="profile-pair-fixture" REPO_ROOT="${ROOT_DIR}" AP_POSTGRES_PORT=5433
  eval "$(awk '/^AP_POSTGRES_HOST=""/,/^\[\[ -n "\$AP_POSTGRES_HOST" \]\]/' "${DEPLOY}")"
  eval "$(awk '/^AP_POSTGRES_POOL_MAX_CONNS=""/,/^\[\[ "\$AP_POSTGRES_POOL_MAX_CONNS"/' "${DEPLOY}")"
  export AP_SSH_HOST="100.100.1.6"
  eval "${write_host_env_fn}"
  write_host_env "${generated_env}"
)
`

func runNativeDeploy(t *testing.T, script string) {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{Script: nativeDeployPrelude + script + "\n(( failures == 0 ))\n", Args: []string{t.TempDir()}})
	harness.Succeeds(t, "native deploy checks", r)
}

const nativeHostEnvScript = `if [[ ! -s "${generated_env}" ]]; then
  record_fail "ap-host-native write_host_env did not produce generated env contents"
elif grep -Eq '^(CACHE_HOST|CACHE_PORT|CACHE_SOCKET_PATH|CACHE_PASSWORD|CACHE_DB|SETTINGS_DIR)=' "${generated_env}"; then
  record_fail "generated host env contents include removed keys"
elif ! grep -Fxq 'POSTGRES_HOST=100.100.1.8' "${generated_env}" || ! grep -Fxq 'POSTGRES_PORT=5433' "${generated_env}"; then
  record_fail "generated host env must use the central PostgreSQL TLS endpoint"
fi
if direct_error="$(
  hostname() { echo kapu; }
  export -f hostname
  AP_POSTGRES_PORT=6000 "$DEPLOY" osaka --dry-run 2>&1
)"; then
  record_fail "ap-host-native deploy accepted a non-TLS PostgreSQL port"
elif [[ "$direct_error" == 'AP_POSTGRES_PORT must be the central PostgreSQL TLS port (5433)' ]]; then
  pass "ap-host-native deploy rejects a non-TLS PostgreSQL port before any build or transport"
else
  record_fail "ap-host-native deploy did not reject a non-TLS PostgreSQL port at entry"
fi
`

const nativeRevisionsScript = `(
  set -euo pipefail
  fixture="$tmp/revisions"; mkdir "$fixture"
  
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
  printf 'native_release_window=W9\n' >"$product/scripts/deploy/lib/ap-host-native-window.sh"
  git -C "$product" -c user.name=fixture -c user.email=fixture@example.invalid commit -qam 'different family'
  AP_NATIVE_PRODUCT_REVISION="$(git -C "$product" rev-parse HEAD)"
  if "$fixture/deploy" osaka --apply 2>"$fixture/result"; then exit 1; fi
  grep -Fq 'native tool/product release window mismatch' "$fixture/result"
  [[ ! -e "$fixture/request" ]]
)
`

const nativeApplyOrderScript = `(
  set -euo pipefail
  fixture="$tmp/apply-order"; mkdir "$fixture"
  
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
    case "$request" in
      'bash -s --') echo 2026-11-22T00:00:00Z ;;
      *' apply-origin '*) printf 'old %040d 1:2:3\n' 0 ;;
      *' same-window '*) echo 12345678-1234-1234-1234-123456789012 ;;
    esac
  }
  rsync() { :; }
  npm() { :; }
  export -f hostname ssh rsync npm
  for outcome in accepted observer-failed; do
    rm -f "$OBSERVED" "$OBSERVED.fail"
    if [[ "$outcome" != accepted ]]; then touch "$OBSERVED.fail"; fi
    status=0
    ARTIFACT_DIR="$fixture/artifact-$outcome" "$fixture/deploy" osaka --apply 2>"$fixture/$outcome.err" || status=$?
    steps="$(sed -nE -e "s/^bash -s -- [^ ]+ [^ ]+ collector-a 30005 [^ ]+ [0-9]+ [0-9]+ $AP_NATIVE_PRODUCT_REVISION (apply-origin|same-window|apply-accepted)( .*)?$/\1/p" \
      -e 's/^observer (.*)$/\1/p' "$OBSERVED" | paste -sd' ')"
    if [[ "$outcome" == accepted ]]; then
      [[ "$status" == 0 && "$steps" == 'apply-origin same-window ap-host-native-status.sh ap-completion-check.sh apply-accepted' ]] ||
        { cat "$fixture/$outcome.err" >&2; printf '%s\n' "$steps" >&2; exit 1; }
    else
      [[ "$status" != 0 && "$steps" == 'apply-origin same-window ap-host-native-status.sh' ]] ||
        { cat "$fixture/$outcome.err" >&2; printf '%s\n' "$steps" >&2; exit 1; }
    fi
  done
) && pass 'osaka apply는 관찰 검사 두 개가 통과한 뒤에만 수용을 요청합니다.' ||
  record_fail 'osaka apply의 원점·창·관찰·수용 순서가 맞지 않습니다.'
`

const nativePermissionsScript = `permission_fn="$(awk '/^normalize_runtime_payload_permissions\(\) \{/,/^}$/' "${REMOTE_APPLY}")"
permission_fixture="$tmp/permissions"; mkdir "$permission_fixture"
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
`

const nativeUnitScript = `unit_copy_dir="$tmp/unit"; mkdir "$unit_copy_dir"
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
`

const nativeTraversalScript = `mkdir -p "${tmp}/bin" "${tmp}/success" "${tmp}/failure"
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
`

const nativeNoCurrentScript = `mkdir -p "${tmp}/bin" "${tmp}/success" "${tmp}/failure"
touch "${tmp}/KR.key"

printf '#!/usr/bin/env bash\nexit 1\n' > "${tmp}/bin/ssh"
printf '#!/usr/bin/env bash\n[[ "$3" == status ]] && exit 0\nexec %q "$@"\n' "$(command -v git)" > "${tmp}/bin/git"
chmod +x "${tmp}/bin/ssh" "${tmp}/bin/git"
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
`

const nativeResolverScript = `. "$ROOT_DIR/scripts/deploy/lib/ap-host-native-window.sh"

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
`

const nativeRollbackValidateScript = `mkdir -p "${tmp}/rollback-bin" "${tmp}/rollback-fixture/bin" \
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
for native_port in 5433 6000 invalid ''; do
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
`

const nativeRollbackRunScript = `mkdir -p "${tmp}/bin" "${tmp}/success" "${tmp}/failure"
touch "${tmp}/KR.key"

printf '#!/usr/bin/env bash\nexit 1\n' > "${tmp}/bin/ssh"
printf '#!/usr/bin/env bash\n[[ "$3" == status ]] && exit 0\nexec %q "$@"\n' "$(command -v git)" > "${tmp}/bin/git"
chmod +x "${tmp}/bin/ssh" "${tmp}/bin/git"
stop_fixture="$tmp/stop"; mkdir "$stop_fixture"
run_native_units() (
  local state="$1" script="$2"
  unit="$slot_collector_unit"
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
  eval "${script}"
)
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
`

func TestHostEnvTargetsTheCentralTLSEndpoint(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeHostEnvScript)
}

func TestToolAndProductRevisionsAreCheckedBeforeTransport(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeRevisionsScript)
}

func TestApplyRequestsAcceptanceOnlyAfterBothObservers(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeApplyOrderScript)
}

func TestPayloadPermissionsNeverFollowSymlinks(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativePermissionsScript)
}

func TestCollectorUnitPassesSystemdAnalyze(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeUnitScript)
}

func TestReleaseIDTraversalIsRejectedBeforeBuild(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeTraversalScript)
}

func TestApplyWithoutAcceptedOriginStopsBeforeBuild(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeNoCurrentScript)
}

func TestReleaseResolverRejectsReuseAndEscape(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeResolverScript)
}

func TestRollbackValidationRequiresAnIntactContract(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeRollbackValidateScript)
}

func TestRollbackStopsIssuerFirstAndHonoursCompletionGate(t *testing.T) {
	t.Parallel()
	runNativeDeploy(t, nativeRollbackRunScript)
}
