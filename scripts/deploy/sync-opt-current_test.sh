#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
QUADLET_BIN=/usr/libexec/podman/quadlet

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

[[ -x "${QUADLET_BIN}" ]] || fail "quadlet generator missing: ${QUADLET_BIN}"
unshare --user --map-root-user --mount true 2>/dev/null || fail "unprivileged user+mount namespace unavailable"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

fakebin="${tmp}/fakebin"
mkdir -p "${fakebin}"
cat >"${fakebin}/hostname" <<'EOF'
#!/usr/bin/env bash
echo hololive-seoul
EOF
cat >"${fakebin}/systemctl" <<'EOF'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
case "$1" in
  is-active) exit 3 ;;
  is-enabled) exit 1 ;;
  show)
    case "$*" in
      *--property=UnitPath*) echo /etc/systemd/system /run/systemd/system ;;
      *--property=NeedDaemonReload*) echo no ;;
      *--property=Names*) if [[ -e "${SYNC_TEST_EVENTS}.alias" ]]; then echo "$2 valkey-alias.service"; else echo "$2"; fi ;;
      *--property=DropInPaths*) if [[ -e "${SYNC_TEST_EVENTS}.alias-dropin" ]]; then echo /etc/systemd/system/valkey-alias.service.d/10-image.conf; fi ;;
      *--property=LoadState*)
        if [[ -e "${SYNC_TEST_EVENTS}.not-found" ]]; then echo not-found
        elif [[ -L /etc/systemd/system/$2 ]]; then echo masked
        else echo loaded; fi ;;
      *--property=FragmentPath*)
        if [[ -L /etc/systemd/system/$2 ]]; then echo "/etc/systemd/system/$2"
        elif [[ ! -e "${SYNC_TEST_EVENTS}.not-found" ]]; then echo "/run/systemd/generator/$2"; fi ;;
    esac
    ;;
esac
EOF
cat >"${fakebin}/chown" <<'EOF'
#!/usr/bin/env bash
printf 'chown %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
EOF
cat >"${fakebin}/mv" <<'EOF'
#!/usr/bin/env bash
if [[ -f "$SYNC_TEST_EVENTS.fail-before" && "${!#}" == "$(<"$SYNC_TEST_EVENTS.fail-before")" ]]; then exit 92; fi
exec /usr/bin/mv "$@"
EOF
cat >"${fakebin}/podman" <<'EOF'
#!/usr/bin/env bash
[[ "$1 $2 $#" == "image exists 3" ]] || exit 64
! grep -qxF -- "$3" "$SYNC_TEST_EVENTS.image-missing" 2>/dev/null
EOF
cat >"${fakebin}/python3" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == '-I /opt/iris-stack/valkey-ha/stage.py selected hololive' ]]; then
  [[ ! -e "$SYNC_TEST_EVENTS.stage-denied" ]]
  exit $?
fi
exec /usr/bin/python3 "$@"
EOF
chmod +x "${fakebin}"/*

slot_table_files=(scripts/deploy/lib/collector-slots.sh scripts/deploy/lib/collector-slots.tsv)
make_repo() {
  local repo="$1" path
  mkdir -p "${repo}"
  for path in \
    scripts/deploy/lib/compose-env.sh scripts/deploy/quadlet-prestart.sh \
    scripts/deploy/sync-opt-current.sh \
    scripts/deploy/quadlet-{redeploy,rollback}-service.sh \
    scripts/deploy/lib/{writer-drain-guard,worker-profile-pair,systemd-stop-guard}.py \
    scripts/deploy/lib/postgres-capacity.sh scripts/ci/postgres-capacity-policy.tsv \
    scripts/deploy/lib/admin-bind.sh scripts/deploy/lib/public-bind-mounts.sh \
    scripts/deploy/lib/postgres-endpoint.sh scripts/deploy/lib/source-revision.sh \
    scripts/systemd/admin-dashboard-ingress-firewall.service scripts/systemd/admin-dashboard-ingress.nft \
    verification/tla/{AlarmDispatchClaim,CollectionJobLease}/COVERAGE.md \
    hololive/hololive-api/scripts/init-db hololive/hololive-api/cmd/db-migrate/queries/bootstrap_writer_admission.sql \
    deploy "${slot_table_files[@]}"; do
    mkdir -p "${repo}/$(dirname "${path}")"
    cp -R "${ROOT_DIR}/${path}" "${repo}/${path}"
  done
  printf '\n' >>"$repo/scripts/deploy/sync-opt-current.sh"
  cat >"${repo}/scripts/deploy/verify-exec-tree-ownership.sh" <<'EOF'
#!/usr/bin/env bash
printf 'verify %s\n' "$#" >>"${SYNC_TEST_EVENTS:?}"
printf 'verify-path %s\n' "$@" >>"${SYNC_TEST_EVENTS:?}"
if [[ -e "${SYNC_TEST_EVENTS}.ownership-denied" ]]; then
  denied="$(cat "${SYNC_TEST_EVENTS}.ownership-denied")"
  for path in "$@"; do [[ "$path" != "$denied" ]] || exit 1; done
fi
EOF
  git -C "${repo}" init -q
  commit_repo "${repo}" fixture
}

commit_repo() {
  git -C "$1" add -A
  git -C "$1" -c user.name=fixture -c user.email=fixture@example.invalid commit -qm "$2"
}

make_host() {
  local host="$1" x_spaces="$2" installed="${1}/etc-containers-systemd/hololive-bot"
  mkdir -p "${host}"/{etc-containers-systemd,usr-local-libexec,etc-stack-secrets/hololive-bot,etc-systemd-system,etc-nftables.d,run-lock}
  mkdir -p "${host}/run/hololive-bot" "${host}/run/lock"
  mkdir -m 0700 "${host}/run/mount"
  printf 'accepted runtime state\n' >"${host}/run/hololive-bot/marker"
  mkdir -p "${installed}/hololive-api.container.d" "${installed}/hololive-alarm-worker.container.d"
  printf '[Container]\nContainerName=stale\nImage=localhost/stale:old\n' >"${installed}/stale.container"
  printf '[Container]\nLabel=io.hololive.test.worker-profile=1\n' \
    >"${installed}/hololive-api.container.d/50-worker-profile.conf"
  printf '# old\n[Container]\nEnvironment=X_SPACES_OLD=1\n' \
    >"${installed}/hololive-alarm-worker.container.d/40-x-spaces.conf"
  mkdir -p "${host}/opt/current"
  printf 'old current\n' >"${host}/opt/current/marker"
  printf 'VALKEY_BIND_IP=100.100.1.8\nHOLOLIVE_X_SPACES_ENABLED=%s\n' \
    "${x_spaces}" >"${host}/etc-stack-secrets/hololive-bot/compose.env"
}

host_snapshot() {
  local host="$1" opt=opt/current
  case "${2:-}" in --application-only|--source-only) opt=opt ;; esac
  (cd "${host}" && find etc-containers-systemd usr-local-libexec etc-systemd-system etc-nftables.d etc-stack-secrets run "$opt" \
    -printf '%p %m %U:%G %i\n' -type f -printf '%p %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort)
}

run_sync() {
  local host="$1" repo="$2" opt="$1/opt"
  shift 2
  [[ ${1:-} != --source-only ]] || opt=/opt
  [[ ${1:-} != --valkey-only || -n ${SYNC_TEST_OPT_ROOT:-} ]] || opt=/opt/hololive-bot/compose
  : >"${host}/events"
  # shellcheck disable=SC2016 # namespace 안의 bash가 확장한다.
  unshare --user --map-root-user --mount bash -c '
    set -euo pipefail
    host="$1"; repo="$2"; stubs="$3"; bootstrap="$4"
    shift 4
    mount --bind "${host}/etc-containers-systemd" /etc/containers/systemd
    mount --bind "${host}/usr-local-libexec" /usr/local/libexec
    mount --bind "${host}/etc-stack-secrets" /etc/stack-secrets
    mount --bind "${host}/etc-systemd-system" /etc/systemd/system
    mount --bind "${host}/etc-nftables.d" /etc/nftables.d
    mount --bind "${host}/run" /run
    mount --bind "${host}/run-lock" /run/lock
    if [[ " $* " != *" --source-only "* ]]; then
      mount -t tmpfs -o mode=0755 tmpfs /opt
      mkdir -p /opt/iris-stack/valkey-ha
      : >/opt/iris-stack/valkey-ha/stage.py
    fi
    if [[ " $* " == *" --valkey-only "* ]]; then
      mkdir -p /opt/hololive-bot/compose
      mount --bind "$host/opt" /opt/hololive-bot/compose
      for directory in /usr/lib/systemd/system /usr/local/lib/systemd/system /usr/share/containers/systemd; do
        [[ ! -d $directory ]] || mount -t tmpfs -o mode=0755 tmpfs "$directory"
      done
    fi
    if [[ " $* " == *" --source-only "* ]]; then
      jail="$host/jail"
      mkdir -p "$jail"
      mount -t tmpfs -o mode=0755 tmpfs "$jail"
      mkdir -p "$jail"/{usr,dev,etc,proc,run,sys,tmp,var,opt,repo,stubs}
      for directory in dev etc proc run sys; do mount --rbind "/$directory" "$jail/$directory"; done
      for directory in bin sbin lib lib64 share libexec local/libexec; do
        mkdir -p "$jail/usr/$directory"
        mount --rbind "/usr/$directory" "$jail/usr/$directory"
      done
      chmod 0755 "$jail/usr" "$jail/usr/local"
      mount --bind "$repo" "$jail/repo"
      mount --bind "$stubs" "$jail/stubs"
      touch "$jail/bootstrap" "$jail/events"
      mount --bind "$bootstrap" "$jail/bootstrap"
      mount --bind "$host/events" "$jail/events"
      if [[ -f "$host/canary" ]]; then touch "$jail/canary"; mount --bind "$host/canary" "$jail/canary"; fi
      if [[ -e "$host/events.fail-before" ]]; then cp "$host/events.fail-before" "$jail/events.fail-before"; fi
      for directory in bin sbin lib lib64; do
        [[ -e "$jail/$directory" ]] || ln -s "usr/$directory" "$jail/$directory"
      done
      mount --bind "$host/opt" "$jail/opt"
      exec chroot "$jail" env PATH=/stubs:/usr/bin:/usr/sbin HOME=/tmp TMPDIR=/tmp SYNC_TEST_EVENTS=/events REPO_ROOT=/repo OPT_ROOT=/opt bash ./bootstrap "${@: -2}"
    fi
    exec "$@"
  ' _ "${host}" "$repo" "$fakebin" "$ROOT_DIR/scripts/deploy/sync-opt-current.sh" env PATH="${fakebin}:${PATH}" SYNC_TEST_EVENTS="${host}/events" \
    REPO_ROOT="${repo}" OPT_ROOT="${SYNC_TEST_OPT_ROOT:-$opt}" \
    bash "${ROOT_DIR}/scripts/deploy/sync-opt-current.sh" "$@" >"${host}/out" 2>&1
}

assert_rejected_without_change() {
  local host="$1" repo="$2" label="$3" expected="${4:-staged Quadlet units do not generate; nothing was installed}" before after
  before="$(host_snapshot "${host}" "${5:-}")"
  if run_sync "${host}" "${repo}" "${@:5}"; then
    cat "${host}/out" >&2
    fail "${label}: installer accepted the invalid deployment"
  fi
  after="$(host_snapshot "${host}" "${5:-}")"
  if [[ "$after" != "$before" ]]; then
    diff -u <(printf '%s\n' "$before") <(printf '%s\n' "$after") >&2 || :
    fail "${label}: host state changed after a rejected dry-run"
  fi
  ! grep -q 'daemon-reload' "${host}/events" || fail "${label}: daemon-reload ran after a rejected dry-run"
  grep -Fq "${expected}" "${host}/out" ||
    { cat "${host}/out" >&2; fail "${label}: rejection did not come from the expected check"; }
  if compgen -G "${host}/opt/.staging-current.*" >/dev/null || compgen -G "${host}/opt/.quadlet-check.*" >/dev/null; then
    fail "${label}: temporary staging directories were left behind"
  fi
}

base_repo="${tmp}/repo-base"
make_repo "${base_repo}"

host="${tmp}/host-unadmitted"
make_host "${host}" 1
touch "${host}/events.stage-denied"
assert_rejected_without_change "${host}" "${base_repo}" "Valkey admission" "Valkey standalone stage is not admitted"

host="${tmp}/host-ok"
make_host "${host}" 1
run_sync "${host}" "${base_repo}" || { cat "${host}/out" >&2; fail "valid units were rejected"; }
installed="${host}/etc-containers-systemd/hololive-bot"
for unit in "${ROOT_DIR}"/deploy/quadlet/hololive-bot/*; do
  cmp -s "${unit}" "${installed}/${unit##*/}" || fail "staged unit not installed: ${unit##*/}"
done
[[ ! -e "${installed}/stale.container" ]] || fail "unit removed from the repository stayed installed"
grep -q 'io.hololive.test.worker-profile=1' "${installed}/hololive-api.container.d/50-worker-profile.conf" ||
  fail "worker-profile drop-in was not preserved"
for unit in hololive-api hololive-alarm-worker; do
  cmp -s "${ROOT_DIR}/deploy/quadlet/optional/x-spaces/${unit}.container.d/40-x-spaces.conf" \
    "${installed}/${unit}.container.d/40-x-spaces.conf" || fail "x-spaces drop-in not installed for ${unit}"
done
[[ -x "${host}/usr-local-libexec/hololive-bot/quadlet-prestart" ]] || fail "prestart helper not installed"
for lib in admin-bind.sh public-bind-mounts.sh postgres-capacity.sh compose-env.sh postgres-endpoint.sh collector-slots.sh collector-slots.tsv; do
  cmp -s "${base_repo}/scripts/deploy/lib/${lib}" "${host}/usr-local-libexec/hololive-bot/lib/${lib}" ||
    fail "prestart dependency not installed: ${lib}"
done
bash -c 'source "$1"; source "$2"; postgres_endpoint_quadlet "$3" "$4"' _ \
  "${host}/usr-local-libexec/hololive-bot/lib/compose-env.sh" \
  "${host}/usr-local-libexec/hololive-bot/lib/postgres-endpoint.sh" \
  "${installed}/hololive-api.container" "${host}/etc-stack-secrets/hololive-bot/compose.env" >/dev/null ||
  fail "installed endpoint library cannot resolve the effective application endpoint"
bash -c 'source "$1"; postgres_capacity_assert_policy_target "$2" "$3"' _ \
  "${host}/usr-local-libexec/hololive-bot/lib/postgres-capacity.sh" \
  "${host}/opt/current/scripts/ci/postgres-capacity-policy.tsv" \
  "${host}/etc-stack-secrets/hololive-bot/compose.env" >/dev/null ||
  fail "installed capacity library cannot validate the connection budget"
installed_validator="${host}/usr-local-libexec/hololive-bot/lib/compose-env.sh"
bash -c '. "$1"; compose_env_validate_file_format "$2"' _ \
  "$installed_validator" "${host}/etc-stack-secrets/hololive-bot/compose.env" ||
  fail "installed validator rejects valid env"
printf 'KEY=value \n' >"${tmp}/invalid-installed.env"
if bash -c '. "$1"; compose_env_validate_file_format "$2"' _ \
  "$installed_validator" "${tmp}/invalid-installed.env" >/dev/null 2>&1; then
  fail "installed validator accepts surrounding value whitespace"
fi
[[ -f "${host}/etc-systemd-system/admin-dashboard-ingress-firewall.service" ]] || fail "firewall unit not installed"
[[ ! -e "${host}/opt/current/marker" && -f "${host}/opt/current/deploy/postgres/pg_hba.conf" ]] ||
  fail "current tree was not replaced by the staged tree"
grep -qx 'systemctl daemon-reload' "${host}/events" || fail "daemon-reload did not run after installation"
[[ "$(cat "${host}/opt/current/.source-revision")" == "$(git -C "${base_repo}" rev-parse HEAD)" ]] ||
  fail "deploy tree does not record the full revision it was archived from"
grep -q '^verify ' "${host}/events" || fail "exec-tree ownership verification did not run"
! compgen -G "${host}/opt/.quadlet-check.*" >/dev/null || fail "Quadlet check directory was left behind"
pass "valid units install with x-spaces drop-ins, keep the worker-profile drop-in and record the source revision"

preserved=$(stat -c '%i:%Y' "$installed/hololive-api.container" "$installed/holo-postgres.container" "$host/opt/current/.source-revision")
backups=$(find "$host/opt" -maxdepth 1 -name 'current.bak-*' -printf '%i\n' | sort)
printf '\n' >>"$installed/valkey-cache.container"
run_sync "$host" "$base_repo" --valkey-only || { cat "$host/out" >&2; fail "Valkey 단독 게시 실패"; }
cmp -s "$installed/valkey-cache.container" "$base_repo/deploy/quadlet/hololive-bot/valkey-cache.container" || fail "Valkey 후보가 게시되지 않았습니다"
[[ $preserved == "$(stat -c '%i:%Y' "$installed/hololive-api.container" "$installed/holo-postgres.container" "$host/opt/current/.source-revision")" &&
   $backups == "$(find "$host/opt" -maxdepth 1 -name 'current.bak-*' -printf '%i\n' | sort)" ]] || fail "Valkey 단독 게시가 다른 리소스를 변경했습니다"
pass "Valkey 단독 게시는 앱·DB·실행 트리·기존 복구점에 손대지 않습니다"

printf '\n' >>"$installed/valkey-cache.container"
sed -i 's/^VALKEY_BIND_IP=.*/VALKEY_BIND_IP=0.0.0.0/' "$host/etc-stack-secrets/hololive-bot/compose.env"
assert_rejected_without_change "$host" "$base_repo" "Valkey wildcard bind" "VALKEY_BIND_IP must select the central Valkey endpoint" --valkey-only
pass "Valkey 단독 게시는 중앙 주소가 아닌 VALKEY_BIND_IP를 거부합니다"

sed -i 's/^VALKEY_BIND_IP=.*/VALKEY_BIND_IP=100.100.1.8/' "$host/etc-stack-secrets/hololive-bot/compose.env"
stale_start="installed Valkey start checks differ from the checkout; run a full sync first"
printf '\n' >>"$host/usr-local-libexec/hololive-bot/quadlet-prestart"
assert_rejected_without_change "$host" "$base_repo" "stale Valkey prestart" "$stale_start" --valkey-only
cp "$base_repo/scripts/deploy/quadlet-prestart.sh" "$host/usr-local-libexec/hololive-bot/quadlet-prestart"
printf '\n' >>"$host/usr-local-libexec/hololive-bot/lib/admin-bind.sh"
assert_rejected_without_change "$host" "$base_repo" "stale Valkey bind check" "$stale_start" --valkey-only
cp "$base_repo/scripts/deploy/lib/admin-bind.sh" "$host/usr-local-libexec/hololive-bot/lib/admin-bind.sh"
printf '\n' >>"$host/opt/current/deploy/topology/hosts.conf"
assert_rejected_without_change "$host" "$base_repo" "stale Valkey topology" "$stale_start" --valkey-only
cp "$base_repo/deploy/topology/hosts.conf" "$host/opt/current/deploy/topology/hosts.conf"
pass "Valkey 단독 게시는 설치된 시작 검사·토폴로지가 checkout과 다르면 거부합니다"

printf 'docker.io/valkey/valkey:9.1.3-alpine3.24\n' >"$host/events.image-missing"
assert_rejected_without_change "$host" "$base_repo" "missing Valkey image" "Valkey image is not present: docker.io/valkey/valkey:9.1.3-alpine3.24" --valkey-only
printf 'VALKEY_IMAGE=localhost/iris-stack-valkey:pinned\n' >>"$host/etc-stack-secrets/hololive-bot/compose.env"
printf 'localhost/iris-stack-valkey:pinned\n' >"$host/events.image-missing"
assert_rejected_without_change "$host" "$base_repo" "missing pinned Valkey image" "Valkey image is not present: localhost/iris-stack-valkey:pinned" --valkey-only
sed -i '/^VALKEY_IMAGE=/d' "$host/etc-stack-secrets/hololive-bot/compose.env"
rm -f -- "$host/events.image-missing"
pass "Valkey 단독 게시는 unit이 실제로 쓸 이미지가 없으면 거부합니다"

printf '\n' >>"$installed/valkey-cache.container"
printf 'VALKEY_IMAGE=\n' >>"$host/etc-stack-secrets/hololive-bot/compose.env"
assert_rejected_without_change "$host" "$base_repo" "empty Valkey image" "VALKEY_IMAGE is empty" --valkey-only
sed -i 's/^VALKEY_IMAGE=.*/VALKEY_IMAGE=""/' "$host/etc-stack-secrets/hololive-bot/compose.env"
assert_rejected_without_change "$host" "$base_repo" "empty quoted Valkey image" "VALKEY_IMAGE is empty" --valkey-only
sed -i 's|^VALKEY_IMAGE=.*|VALKEY_IMAGE=localhost/iris-stack-valkey:pinned|' "$host/etc-stack-secrets/hololive-bot/compose.env"
printf 'docker.io/valkey/valkey:9.1.3-alpine3.24\n' >"$host/events.image-missing"
run_sync "$host" "$base_repo" --valkey-only || { cat "$host/out" >&2; fail "compose.env에 고정한 Valkey 이미지로 단독 게시 실패"; }
cmp -s "$installed/valkey-cache.container" "$base_repo/deploy/quadlet/hololive-bot/valkey-cache.container" || fail "고정 이미지 Valkey 후보가 게시되지 않았습니다"
sed -i '/^VALKEY_IMAGE=/d' "$host/etc-stack-secrets/hololive-bot/compose.env"
rm -f -- "$host/events.image-missing"
pass "Valkey 단독 게시는 비어 있는 VALKEY_IMAGE를 거부하고 compose.env에 고정한 이미지를 확인합니다"

for path in /usr/local/libexec/hololive-bot/quadlet-prestart /usr/local/libexec/hololive-bot/lib/collector-slots.tsv \
  /opt/hololive-bot/compose/current/deploy/topology/hosts.conf /opt/iris-stack/valkey-ha/stage.py; do
  grep -qxF "verify-path $path" "$host/events" || fail "Valkey 시작 검사 경로의 소유권을 확인하지 않았습니다: $path"
done
printf '\n' >>"$installed/valkey-cache.container"
start_check="$host/usr-local-libexec/hololive-bot/lib/collector-slots.tsv"
printf '\n' >>"$start_check"
assert_rejected_without_change "$host" "$base_repo" "stale Valkey slot table" "$stale_start" --valkey-only
cp "$base_repo/scripts/deploy/lib/collector-slots.tsv" "$start_check"
chmod 0664 "$start_check"
assert_rejected_without_change "$host" "$base_repo" "Valkey start check mode" "$stale_start" --valkey-only
chmod 0644 "$start_check"
mv "$start_check" "$start_check.real"
ln -s collector-slots.tsv.real "$start_check"
assert_rejected_without_change "$host" "$base_repo" "symlinked Valkey start check" "$stale_start" --valkey-only
rm "$start_check"
mv "$start_check.real" "$start_check"
podman unshare /usr/bin/chown 1 "$start_check"
assert_rejected_without_change "$host" "$base_repo" "non-root Valkey start check" "$stale_start" --valkey-only
podman unshare /usr/bin/chown 0 "$start_check"
printf '%s\n' /usr/local/libexec/hololive-bot/lib/collector-slots.tsv >"$host/events.ownership-denied"
assert_rejected_without_change "$host" "$base_repo" "unsafe Valkey start check path" "installed Valkey start checks are writable by a non-root user" --valkey-only
printf '%s\n' /opt/iris-stack/valkey-ha/stage.py >"$host/events.ownership-denied"
assert_rejected_without_change "$host" "$base_repo" "unsafe Valkey stage tool path" "Valkey stage tool is missing or writable by a non-root user" --valkey-only
assert_rejected_without_change "$host" "$base_repo" "unsafe Valkey stage tool path in a full sync" "Valkey stage tool is missing or writable by a non-root user"
rm "$host/events.ownership-denied"
SYNC_TEST_OPT_ROOT="$host/opt" assert_rejected_without_change "$host" "$base_repo" "other Valkey runtime tree" \
  "Valkey-only publication checks the runtime tree that the prestart reads" --valkey-only
pass "Valkey 단독 게시는 root 소유·모드·심볼릭 링크·상위 경로·실제 실행 트리가 다른 시작 검사를 거부합니다"

mkdir "$installed/valkey-cache.container.d"
printf '[Container]\nSecurityLabelLevel=s0:c900,c901\n' >"$installed/valkey-cache.container.d/50-mcs-level.conf"
assert_rejected_without_change "$host" "$base_repo" "local Valkey drop-in" "expected an existing Valkey unit without local drop-ins" --valkey-only
rm -r "$installed/valkey-cache.container.d"
override="unsupported Quadlet/systemd override origin"
mkdir "$host/etc-systemd-system/valkey-cache.service.d"
printf '[Service]\nExecStartPre=\n' >"$host/etc-systemd-system/valkey-cache.service.d/override.conf"
assert_rejected_without_change "$host" "$base_repo" "Valkey service drop-in" "$override" --valkey-only
rm -r "$host/etc-systemd-system/valkey-cache.service.d"
touch "$host/events.alias-dropin"
assert_rejected_without_change "$host" "$base_repo" "Valkey alias drop-in" "$override" --valkey-only
rm "$host/events.alias-dropin"
touch "$host/events.alias"
assert_rejected_without_change "$host" "$base_repo" "Valkey alias name" "$override" --valkey-only
rm "$host/events.alias"
touch "$host/events.not-found"
assert_rejected_without_change "$host" "$base_repo" "Valkey unit not loaded by systemd" "$override" --valkey-only
rm "$host/events.not-found"
ln -s /dev/null "$host/etc-systemd-system/valkey-cache.service"
ln -s /run/systemd/generator/valkey-cache.service "$host/etc-systemd-system/valkey-alias.service"
assert_rejected_without_change "$host" "$base_repo" "masked Valkey unit with an alias" "$override" --valkey-only
rm "$host/etc-systemd-system/valkey-alias.service"
for alias_target in /etc/systemd/system/held/valkey-cache.service held/valkey-cache.service \
  /etc/systemd/system/multi-user.target.wants/valkey-cache.service ../system/held/../valkey-cache.service \
  held/valkey-cache.service/ /etc/systemd/system/held/valkey-cache.service/. held/valkey-cache.service//; do
  ln -s "$alias_target" "$host/etc-systemd-system/valkey-alias.service"
  assert_rejected_without_change "$host" "$base_repo" "masked Valkey unit with an alias to $alias_target" "$override" --valkey-only
  rm "$host/etc-systemd-system/valkey-alias.service"
done
mkdir -p "$host/run/systemd/system"
ln -s /dev/null "$host/run/systemd/system/valkey-cache.service"
assert_rejected_without_change "$host" "$base_repo" "Valkey runtime mask under the persistent mask" "$override" --valkey-only
rm "$host/run/systemd/system/valkey-cache.service"
ln -sfn /run/systemd/generator/valkey-cache.service "$host/etc-systemd-system/valkey-cache.service"
assert_rejected_without_change "$host" "$base_repo" "Valkey mask not pointing to /dev/null" "$override" --valkey-only
ln -sfn /dev/null "$host/etc-systemd-system/valkey-cache.service"
run_sync "$host" "$base_repo" --valkey-only || { cat "$host/out" >&2; fail "영속 mask만 있는 Valkey unit의 단독 게시 실패"; }
rm "$host/etc-systemd-system/valkey-cache.service"
printf '\n' >>"$installed/valkey-cache.container"
mkdir "$tmp/linked-unit"
printf '[Service]\nExecStart=/bin/true\n' >"$tmp/linked-unit/valkey-cache.service"
ln -s "$tmp/linked-unit/valkey-cache.service" "$host/etc-systemd-system/staging-cache.service"
run_sync "$host" "$base_repo" --valkey-only || { cat "$host/out" >&2; fail "검색 경로 밖의 같은 이름 파일을 가리키는 별도 linked unit이 있는 Valkey 단독 게시 실패"; }
rm "$host/etc-systemd-system/staging-cache.service"
printf '\n' >>"$installed/valkey-cache.container"
mkdir "$host/etc-containers-systemd/container.d"
printf '[Container]\nImage=localhost/other:tag\n' >"$host/etc-containers-systemd/container.d/60-image.conf"
assert_rejected_without_change "$host" "$base_repo" "inherited Quadlet drop-in" "$override" --valkey-only
rm -r "$host/etc-containers-systemd/container.d"
mkdir "$host/etc-containers-systemd/valkey-cache.container.d"
printf '[Container]\nSecurityLabelLevel=s0:c900,c901\n' >"$host/etc-containers-systemd/valkey-cache.container.d/50-mcs-level.conf"
run_sync "$host" "$base_repo" --valkey-only || { cat "$host/out" >&2; fail "호스트 보안 MCS drop-in이 있는 Valkey 단독 게시 실패"; }
rm -r "$host/etc-containers-systemd/valkey-cache.container.d"
pass "Valkey 단독 게시는 MCS 범주만 정하는 호스트 drop-in 말고는 Quadlet·systemd override를 거부합니다"

broken_repo="${tmp}/repo-broken-unit"
cp -R "${base_repo}" "${broken_repo}"
printf '[Container]\nContainerName=broken\n' >"${broken_repo}/deploy/quadlet/hololive-bot/broken.container"
commit_repo "${broken_repo}" broken-unit
host="${tmp}/host-broken-unit"
make_host "${host}" 1
assert_rejected_without_change "${host}" "${broken_repo}" "broken staged unit"
pass "a staged unit that does not generate leaves /opt, prestart, units and daemon state untouched"

dropin_repo="${tmp}/repo-broken-dropin"
cp -R "${base_repo}" "${dropin_repo}"
printf '[Container]\nUnknownKeyForSyncTest=1\n' \
  >>"${dropin_repo}/deploy/quadlet/optional/x-spaces/hololive-api.container.d/40-x-spaces.conf"
commit_repo "${dropin_repo}" broken-dropin
host="${tmp}/host-broken-dropin"
make_host "${host}" 1
assert_rejected_without_change "${host}" "${dropin_repo}" "broken x-spaces drop-in"
host="${tmp}/host-dropin-disabled"
make_host "${host}" 0
run_sync "${host}" "${dropin_repo}" || { cat "${host}/out" >&2; fail "disabled x-spaces drop-in was still checked"; }
installed="${host}/etc-containers-systemd/hololive-bot"
[[ ! -e "${installed}/hololive-api.container.d/40-x-spaces.conf" &&
  ! -e "${installed}/hololive-alarm-worker.container.d/40-x-spaces.conf" ]] ||
  fail "x-spaces drop-in stayed installed with HOLOLIVE_X_SPACES_ENABLED=0"
pass "x-spaces drop-ins are part of the staged check only when enabled"

host="${tmp}/host-capacity"
make_host "${host}" 0
printf 'BOT_POSTGRES_POOL_MAX_CONNS=40\n' >>"${host}/etc-stack-secrets/hololive-bot/compose.env"
assert_rejected_without_change "${host}" "${base_repo}" "connection budget" \
  "PostgreSQL connection budget check failed"
pass "an exhausted PostgreSQL connection budget leaves /opt, prestart, units and daemon state untouched"

host="${tmp}/host-preserved-runtime"
make_host "${host}" 0
for path in data logs backups runtime-config artifacts/worker-profile; do
  mkdir -p "${host}/opt/current/${path}"
  printf 'runtime-owned\n' >"${host}/opt/current/${path}/retained"
  chmod 0660 "${host}/opt/current/${path}/retained"
done
mkdir -p "${base_repo}/runtime-config"
printf 'unreviewed\n' >"${base_repo}/runtime-config/unreviewed"
runtime_snapshot() {
  (cd "$1/opt/current" && find data logs backups runtime-config artifacts/worker-profile \
    -printf '%p %m %U:%G\n' -type f -exec sha256sum {} + | LC_ALL=C sort)
}
runtime_before="$(runtime_snapshot "${host}")"
run_sync "${host}" "${base_repo}" --preserve-runtime ||
  { cat "${host}/out" >&2; fail "runtime-preserving sync failed"; }
[[ "$(runtime_snapshot "${host}")" == "${runtime_before}" ]] ||
  fail "artifact-only sync changed runtime bytes, ownership or mode"
for backup in "${host}/opt/"current.bak-*; do
  [[ ! -e "${backup}/runtime-config" ]] || fail "artifact-only backup copied runtime configuration"
done
pass "artifact-only sync preserves runtime bytes and metadata without copying runtime configuration"

migrator_path=deploy/quadlet/hololive-bot/hololive-db-migrate.container
w1_repo="${tmp}/repo-pool"
cp -a "${base_repo}" "${w1_repo}"
sed -e 's/^Exec=--schema-only$/Exec=--configure-login-roles/' \
  -e 's/ hololive-db-migrate schema$/ hololive-db-migrate admin-bootstrap/' \
  "${base_repo}/${migrator_path}" >"${w1_repo}/${migrator_path}"
commit_repo "${w1_repo}" pool-window
w1_host="${tmp}/host-pool"
make_host "${w1_host}" 1
run_sync "${w1_host}" "${w1_repo}" || { cat "${w1_host}/out" >&2; fail "pool-window installation failed"; }
for path in data logs backups runtime-config artifacts/worker-profile; do
  mkdir -p "${w1_host}/opt/current/${path}"
  printf 'accepted runtime\n' >"${w1_host}/opt/current/${path}/accepted"
done
touch -d @1 "${w1_host}/opt/current/deploy/postgres/pg_hba.conf" \
  "${w1_host}/opt/current/hololive/hololive-api/scripts/init-db/"*

protected_snapshot() (
  cd "$1"
  find etc-containers-systemd usr-local-libexec etc-systemd-system etc-nftables.d etc-stack-secrets run/hololive-bot \
    opt/current/{deploy/{postgres,nginx,topology},hololive/hololive-api/scripts/init-db,data,logs,backups,runtime-config,artifacts/worker-profile} \
    -type f ! -name hololive-db-migrate.container -printf '%p %i %m %U:%G %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort
)
w2_repo="${tmp}/repo-card"
cp -a "${base_repo}" "$w2_repo"
printf 'card release\n' >"$w2_repo/hololive/hololive-api/VERSION"
commit_repo "$w2_repo" writer-drain
host="${tmp}/host-card"
cp -a "$w1_host" "$host"
before="$(protected_snapshot "$host")"
run_sync "$host" "$w2_repo" --application-only || { cat "$host/out" >&2; fail "writer-drain publication failed"; }
[[ "$(protected_snapshot "$host")" == "$before" ]] || fail "writer drain changed a protected inode, mode or content"
! grep -q '^systemctl ' "$host/events" || fail "source publication changed daemon state"
cmp "$w2_repo/$migrator_path" "$host/etc-containers-systemd/hololive-bot/${migrator_path##*/}"
cmp "$w2_repo/$migrator_path" "$host/opt/current/$migrator_path"
cmp "$w2_repo/hololive/hololive-api/VERSION" "$host/opt/current/hololive/hololive-api/VERSION"
[[ "$(<"$host/opt/current/.source-revision")" == "$(git -C "$w2_repo" rev-parse HEAD)" ]] ||
  fail "card deployment revision was not published"
backups=("$host/opt/current.bak-$(git -C "$w2_repo" rev-parse --short HEAD)-"*)
[[ "${#backups[@]}" == 1 && ! -e "${backups[0]}/runtime-config" && ! -e "${backups[0]}/data" ]] ||
  fail "card rollback copy included runtime data"
cmp "$w1_repo/$migrator_path" "${backups[0]}/$migrator_path"
pass "application-only publishes the card tree and schema migrator without touching accepted pool/runtime inodes"

source_host="$tmp/host-source"
source_repo="$tmp/repo-source"
cp -a "$host" "$source_host"
cp -a "$w2_repo" "$source_repo"
installed_revision="$(git -C "$source_repo" rev-parse HEAD)"
chmod -R go-w "$source_host/opt/current/scripts" "$source_host/opt/current/hololive"
chmod go-w "$source_host/opt" "$source_host/opt/current"
chmod go-w "$source_host/usr-local-libexec"
cp "$ROOT_DIR/scripts/deploy/sync-opt-current.sh" "$source_repo/scripts/deploy/sync-opt-current.sh"
printf 'new release\n' >"$source_repo/hololive/hololive-api/VERSION"
commit_repo "$source_repo" new-release
source_revision="$(git -C "$source_repo" rev-parse HEAD)"
assert_rejected_without_change "$source_host" "$source_repo" "stale precursor" "installed revision mismatch" --source-only "$source_revision"
printf 'foreign\n' >"$source_repo/untracked"
assert_rejected_without_change "$source_host" "$source_repo" "unreviewed source" "clean checkout" --source-only "$installed_revision"
rm "$source_repo/untracked"
bad_host="$tmp/host-source-drift"
cp -a "$source_host" "$bad_host"
printf 'foreign\n' >>"$bad_host/opt/current/hololive/hololive-api/VERSION"
assert_rejected_without_change "$bad_host" "$source_repo" "unreviewed precursor bytes" "content or path mismatch" --source-only "$installed_revision"
bad_repo="$tmp/repo-source-deleted"
cp -a "$source_repo" "$bad_repo"
rm "$bad_repo/hololive/hololive-api/VERSION"
commit_repo "$bad_repo" remove-source
assert_rejected_without_change "$source_host" "$bad_repo" "removed source" "regular-file modifications" --source-only "$installed_revision"
bad_repo="$tmp/repo-source-reserved"
cp -a "$source_repo" "$bad_repo"
printf 'retained\n' >"$source_host/canary"
ln -s /canary "$bad_repo/.source-revision"
commit_repo "$bad_repo" reserved-stamp
assert_rejected_without_change "$source_host" "$bad_repo" "reserved stamp" "reserved source-revision" --source-only "$installed_revision"
[[ "$(<"$source_host/canary")" == retained ]] || fail "reserved path changed an external file"
bad_repo="$tmp/repo-source-protected"
cp -a "$source_repo" "$bad_repo"
printf '%s\n' 'echo ran >/canary' >>"$bad_repo/scripts/deploy/verify-exec-tree-ownership.sh"
commit_repo "$bad_repo" protected-change
assert_rejected_without_change "$source_host" "$bad_repo" "changed install helper" "unapproved source-only path" --source-only "$installed_revision"
[[ "$(<"$source_host/canary")" == retained ]] || fail "unapproved helper ran before its rejection"
for attack in writable-parent nonroot-owner file-link acl; do
  bad_host="$tmp/host-source-unsafe-$attack"
  cp -a "$source_host" "$bad_host"
  entry="$bad_host/opt/current/scripts/deploy/quadlet-redeploy-service.sh"
  case "$attack" in
    writable-parent) chmod g+w "${entry%/*}" ;;
    nonroot-owner) podman unshare /usr/bin/chown 1 "$entry" ;;
    file-link) ln "$entry" "$bad_host/alias" ;;
    acl) python3 - "$bad_host/usr-local-libexec/hololive-bot" <<'PY'
import os
import struct
import sys

entries = ((1, 7, -1), (2, 6, os.getuid()), (4, 5, -1), (16, 5, -1), (32, 5, -1))
os.setxattr(sys.argv[1], "system.posix_acl_access", struct.pack("<I", 2) + b"".join(struct.pack("<HHi", *entry) for entry in entries))
PY
    ;;
  esac
  assert_rejected_without_change "$bad_host" "$source_repo" "$attack" "unsafe installed coordinator authority" --source-only "$installed_revision"
done
failed_host="$tmp/host-source-partial"
cp -a "$source_host" "$failed_host"
printf '%s\n' /opt/current/scripts/deploy/sync-opt-current.sh >"$failed_host/events.fail-before"
if run_sync "$failed_host" "$source_repo" --source-only "$installed_revision"; then fail "partial publication reported success"; fi
cmp "$source_repo/hololive/hololive-api/VERSION" "$failed_host/opt/current/hololive/hololive-api/VERSION"
cmp "$w2_repo/scripts/deploy/sync-opt-current.sh" "$failed_host/opt/current/scripts/deploy/sync-opt-current.sh"
[[ "$(<"$failed_host/opt/current/.source-revision")" == "incomplete $installed_revision -> $source_revision" ]] || fail "partial publication retained an actionable stamp"
backups=("$failed_host/opt/current.bak-$(git -C "$source_repo" rev-parse --short HEAD)-"*)
cmp "$w2_repo/hololive/hololive-api/VERSION" "${backups[0]}/hololive/hololive-api/VERSION"
assert_rejected_without_change "$failed_host" "$source_repo" "partial publication replay" "installed revision mismatch" --source-only "$installed_revision"
source_protected() {
  protected_snapshot "$1"
  stat -c '%i %u:%g %a %y %z' "$1/etc-containers-systemd/hololive-bot/${migrator_path##*/}" "$1/opt/current/$migrator_path"
}
before="$(source_protected "$source_host")"
run_sync "$source_host" "$source_repo" --source-only "$installed_revision" || { cat "$source_host/out" >&2; fail "source-only failed"; }
[[ "$(source_protected "$source_host")" == "$before" ]] || fail "source-only replaced protected metadata or bytes"
! grep -q '^systemctl ' "$source_host/events" || fail "source-only invoked systemctl"
[[ "$(<"$source_host/opt/current/.source-revision")" == "$source_revision" ]] || fail "source revision not published"
backups=("$source_host/opt/current.bak-$(git -C "$source_repo" rev-parse --short HEAD)-"*)
[[ "${#backups[@]}" == 1 && "$(<"${backups[0]}/.source-revision")" == "$installed_revision" ]] || fail "missing source recovery point"
for path in scripts/deploy/sync-opt-current.sh hololive/hololive-api/VERSION; do
  cmp "$source_repo/$path" "$source_host/opt/current/$path"
  cmp "$w2_repo/$path" "${backups[0]}/$path"
done
pass "source-only publishes existing app files from the proven precursor without replacing installed definitions or bind inodes"

for path in opt/current/deploy/postgres/pg_hba.conf \
    opt/current/hololive/hololive-api/scripts/init-db/00-assert-pg19-runtime.sql \
    etc-containers-systemd/hololive-bot/{hololive-api,holo-postgres,hololive-alarm-worker}.container \
    etc-containers-systemd/hololive-bot/hololive-api.container.d/40-x-spaces.conf \
    usr-local-libexec/hololive-bot/quadlet-prestart etc-nftables.d/admin-dashboard-ingress.nft; do
  host="$(mktemp -d "${tmp}/host-drift.XXXXXX")"
  cp -a "$w1_host/." "$host/"
  printf '\n# drift\n' >>"$host/$path"
  assert_rejected_without_change "$host" "$w2_repo" "$path" "application-only" --application-only
done
host="${tmp}/host-override"
cp -a "$w1_host" "$host"
dropin="$host/etc-containers-systemd/hololive-bot/hololive-db-migrate.container.d"
mkdir -p "$dropin"
printf '[Container]\nExec=--configure-login-roles\n' >"$dropin/60-command.conf"
assert_rejected_without_change "$host" "$w2_repo" "migrator override" "requires the schema-only migrator" --application-only
assert_rejected_without_change "$w1_host" "$w1_repo" "pool candidate" "requires the schema-only migrator" --application-only
drift_repo="${tmp}/repo-infra-change"
cp -a "$w2_repo" "$drift_repo"
printf 'drift\n' >"$drift_repo/deploy/postgres/unreviewed.conf"
commit_repo "$drift_repo" changed-infrastructure
assert_rejected_without_change "$w1_host" "$drift_repo" "candidate infrastructure" "application-only tree drift" --application-only
pass "application-only rejects candidate, installed, drop-in and privileged bind drift before backup or publication"

host="${tmp}/host-retained-backups"
make_host "${host}" 0
for number in 1 2 3 4; do
  mkdir -p "${host}/opt/current.bak-old-${number}"
  printf 'retained-%s\n' "${number}" >"${host}/opt/current.bak-old-${number}/marker"
done
run_sync "${host}" "${base_repo}" || { cat "${host}/out" >&2; fail "first retained-backup sync failed"; }
initial_recovery=""
for backup in "${host}/opt/"current.bak-*; do
  if [[ -f "${backup}/marker" && "$(<"${backup}/marker")" == "old current" ]]; then
    initial_recovery="${backup}"
  fi
done
[[ -f "${initial_recovery}/marker" && "$(<"${initial_recovery}/marker")" == "old current" ]] ||
  fail "first sync did not retain the original execution tree"
run_sync "${host}" "${base_repo}" || { cat "${host}/out" >&2; fail "repeated retained-backup sync failed"; }
[[ -f "${initial_recovery}/marker" && "$(<"${initial_recovery}/marker")" == "old current" ]] ||
  fail "same-revision sync replaced the original recovery point"
for number in 1 2 3 4; do
  [[ "$(cat "${host}/opt/current.bak-old-${number}/marker")" == "retained-${number}" ]] ||
    fail "repeated sync changed a previous backup"
done
source_recovery=""
for backup in "${host}/opt/"current.bak-*; do
  if [[ -f "${backup}/.source-revision" ]]; then
    source_recovery="${backup}"
  fi
done
[[ -f "${source_recovery}/.source-revision" &&
  "$(<"${source_recovery}/.source-revision")" == "$(git -C "${base_repo}" rev-parse HEAD)" ]] ||
  fail "repeated sync did not retain the previously deployed source"
pass "repeated sync retains every earlier recovery point"

for invalid_line in 'SPACE= value' 'SPACE=value ' $'COMMAND=$(id)' $'COMMAND=`id`' $'CONTROL=a\x01b'; do
  host="${tmp}/host-invalid-env"
  mkdir -p "$host"
  make_host "$host" 1
  printf '%s\n' "$invalid_line" >>"${host}/etc-stack-secrets/hololive-bot/compose.env"
  assert_rejected_without_change "$host" "$base_repo" "invalid compose env" "[ERROR]"
  rm -rf "$host"
done
pass "unsafe compose values are rejected before changing the installed tree"

host="${tmp}/host-writer-lock"
make_host "$host" 0
exec 8>"${host}/run-lock/hololive-quadlet-redeploy.lock"
flock 8
assert_rejected_without_change "$host" "$base_repo" "central writer lock" "another central Quadlet writer is running"
assert_rejected_without_change "$host" "$base_repo" "card writer lock" "another central Quadlet writer is running" --application-only
# shellcheck disable=SC2016 # namespace 안의 bash가 확장한다.
if unshare --user --map-root-user --mount bash -c '
  set -euo pipefail
  mount --bind "$1/run-lock" /run/lock
  { . "$2/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble c; cat "$2/scripts/deploy/lib/po-central-remote.sh"; } |
    bash -s -- c deploy \
    /opt/hololive-bot/compose/po-staging-20261118T000000Z \
    /opt/hololive-bot/compose/backups/card-po-c-20261118T000000Z \
    0123456789abcdef0123456789abcdef01234567 1.0.0 -
' _ "$host" "$ROOT_DIR" >"${host}/collector-out" 2>&1; then
  fail "collector c ran while another central writer held the lock"
fi
grep -q 'another Quadlet writer is running' "${host}/collector-out" || fail "collector c did not reject the central writer lock"
exec 8>&-
pass "source sync and collector c reject a concurrently held central writer lock"

echo "all sync-opt-current checks passed"
