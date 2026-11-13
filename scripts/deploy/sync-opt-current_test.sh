#!/usr/bin/env bash
# sync-opt-current.sh 행동 테스트. 실제 설치기를 user·mount namespace의 root로 실행하고, 설치 경로
# (/etc/containers/systemd, /usr/local/libexec, /etc/stack-secrets, /etc/systemd/system, /etc/nftables.d)는
# 그 namespace 안에서만 임시 디렉터리로 가린다. staging 검사는 실제 quadlet 생성기가 한다.
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
# unit 상태 조회는 inactive·disabled로 보고, 나머지 호출은 기록만 한다.
cat >"${fakebin}/systemctl" <<'EOF'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
case "$1" in
  is-active) exit 3 ;;
  is-enabled) exit 1 ;;
esac
EOF
# map-root-user namespace에는 uid 1000이 없어 런타임 데이터 chown을 기록만 한다.
cat >"${fakebin}/chown" <<'EOF'
#!/usr/bin/env bash
printf 'chown %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
EOF
chmod +x "${fakebin}"/*

# 설치기가 git archive로 읽는 배포 원본만 담은 fixture 저장소를 만든다.
make_repo() {
  local repo="$1" path
  mkdir -p "${repo}"
  for path in \
    scripts/deploy/lib/compose-env.sh scripts/deploy/quadlet-prestart.sh \
    scripts/deploy/lib/postgres-capacity.sh scripts/ci/postgres-capacity-policy.tsv \
    scripts/deploy/lib/admin-bind.sh scripts/deploy/lib/public-bind-mounts.sh \
    scripts/systemd/admin-dashboard-ingress-firewall.service scripts/systemd/admin-dashboard-ingress.nft \
    scripts/systemd/admin-dashboard-ingress-firewall-requires.conf \
    deploy/postgres/pg_hba.conf deploy/nginx/admin-dashboard-ingress.conf.template \
    deploy/quadlet/hololive-bot deploy/quadlet/optional; do
    mkdir -p "${repo}/$(dirname "${path}")"
    cp -R "${ROOT_DIR}/${path}" "${repo}/${path}"
  done
  # 실행 트리 소유권 검사는 실제 호스트 경로 chain을 보므로 호출 인자만 기록한다.
  cat >"${repo}/scripts/deploy/verify-exec-tree-ownership.sh" <<'EOF'
#!/usr/bin/env bash
printf 'verify %s\n' "$#" >>"${SYNC_TEST_EVENTS:?}"
EOF
  git -C "${repo}" init -q
  commit_repo "${repo}" fixture
}

commit_repo() {
  git -C "$1" add -A
  git -C "$1" -c user.name=fixture -c user.email=fixture@example.invalid commit -qm "$2"
}

# 호스트 경로 대역과 이전 설치 상태(옛 unit, worker profile drop-in, 옛 x-spaces drop-in, 이전 current)를 만든다.
make_host() {
  local host="$1" x_spaces="$2" installed="${1}/etc-containers-systemd/hololive-bot"
  mkdir -p "${host}"/{etc-containers-systemd,usr-local-libexec,etc-stack-secrets/hololive-bot,etc-systemd-system,etc-nftables.d}
  mkdir -p "${installed}/hololive-api.container.d" "${installed}/hololive-alarm-worker.container.d"
  printf '[Container]\nContainerName=stale\nImage=localhost/stale:old\n' >"${installed}/stale.container"
  printf '[Container]\nLabel=io.hololive.test.worker-profile=1\n' \
    >"${installed}/hololive-api.container.d/50-worker-profile.conf"
  printf '# old\n[Container]\nEnvironment=X_SPACES_OLD=1\n' \
    >"${installed}/hololive-alarm-worker.container.d/40-x-spaces.conf"
  mkdir -p "${host}/opt/current"
  printf 'old current\n' >"${host}/opt/current/marker"
  printf 'HOLOLIVE_VALKEY_HA_ENABLED=1\nVALKEY_SENTINEL_ADDRS=100.100.1.3:26379,100.100.1.8:26379\nHOLOLIVE_X_SPACES_ENABLED=%s\n' \
    "${x_spaces}" >"${host}/etc-stack-secrets/hololive-bot/compose.env"
}

host_snapshot() {
  local host="$1"
  (cd "${host}" && find etc-containers-systemd usr-local-libexec etc-systemd-system etc-nftables.d opt/current \
    -printf '%p %m\n' -type f -exec sha256sum {} + | LC_ALL=C sort)
}

run_sync() {
  local host="$1" repo="$2"
  shift 2
  : >"${host}/events"
  # shellcheck disable=SC2016 # namespace 안의 bash가 확장한다.
  unshare --user --map-root-user --mount bash -c '
    set -euo pipefail
    host="$1"
    shift
    mount --bind "${host}/etc-containers-systemd" /etc/containers/systemd
    mount --bind "${host}/usr-local-libexec" /usr/local/libexec
    mount --bind "${host}/etc-stack-secrets" /etc/stack-secrets
    mount --bind "${host}/etc-systemd-system" /etc/systemd/system
    mount --bind "${host}/etc-nftables.d" /etc/nftables.d
    exec "$@"
  ' _ "${host}" env PATH="${fakebin}:${PATH}" SYNC_TEST_EVENTS="${host}/events" \
    REPO_ROOT="${repo}" OPT_ROOT="${host}/opt" \
    bash "${ROOT_DIR}/scripts/deploy/sync-opt-current.sh" "$@" >"${host}/out" 2>&1
}

assert_rejected_without_change() {
  local host="$1" repo="$2" label="$3" expected="${4:-staged Quadlet units do not generate; nothing was installed}" before
  before="$(host_snapshot "${host}")"
  if run_sync "${host}" "${repo}"; then
    cat "${host}/out" >&2
    fail "${label}: installer accepted units that do not generate"
  fi
  [[ "$(host_snapshot "${host}")" == "${before}" ]] || fail "${label}: host state changed after a rejected dry-run"
  ! grep -q 'daemon-reload' "${host}/events" || fail "${label}: daemon-reload ran after a rejected dry-run"
  grep -Fq "${expected}" "${host}/out" ||
    { cat "${host}/out" >&2; fail "${label}: rejection did not come from the expected check"; }
  if compgen -G "${host}/opt/.staging-current.*" >/dev/null || compgen -G "${host}/opt/.quadlet-check.*" >/dev/null; then
    fail "${label}: temporary staging directories were left behind"
  fi
}

base_repo="${tmp}/repo-base"
make_repo "${base_repo}"

# 1. 정상 설치: staged unit·x-spaces drop-in이 설치되고 설치기가 소유하지 않는 drop-in은 보존된다.
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
[[ -f "${host}/etc-systemd-system/admin-dashboard-ingress-firewall.service" ]] || fail "firewall unit not installed"
for unit in chatbotgo-postgres twentyq-postgres; do
  cmp -s "${ROOT_DIR}/scripts/systemd/admin-dashboard-ingress-firewall-requires.conf" \
    "${host}/etc-systemd-system/${unit}.service.d/50-admin-dashboard-ingress-firewall.conf" ||
    fail "firewall dependency drop-in not installed for ${unit}"
done
[[ ! -e "${host}/opt/current/marker" && -f "${host}/opt/current/deploy/postgres/pg_hba.conf" ]] ||
  fail "current tree was not replaced by the staged tree"
grep -qx 'systemctl daemon-reload' "${host}/events" || fail "daemon-reload did not run after installation"
[[ "$(cat "${host}/opt/current/.source-revision")" == "$(git -C "${base_repo}" rev-parse HEAD)" ]] ||
  fail "deploy tree does not record the full revision it was archived from"
grep -q '^verify ' "${host}/events" || fail "exec-tree ownership verification did not run"
! compgen -G "${host}/opt/.quadlet-check.*" >/dev/null || fail "Quadlet check directory was left behind"
pass "valid units install with x-spaces drop-ins, keep the worker-profile drop-in and record the source revision"

# 2. 생성되지 않는 staged unit은 아무것도 설치하지 않는다.
broken_repo="${tmp}/repo-broken-unit"
cp -R "${base_repo}" "${broken_repo}"
printf '[Container]\nContainerName=broken\n' >"${broken_repo}/deploy/quadlet/hololive-bot/broken.container"
commit_repo "${broken_repo}" broken-unit
host="${tmp}/host-broken-unit"
make_host "${host}" 1
assert_rejected_without_change "${host}" "${broken_repo}" "broken staged unit"
pass "a staged unit that does not generate leaves /opt, prestart, units and daemon state untouched"

# 3. x-spaces drop-in은 켜졌을 때만 검사 대상이며, 생성 실패 시 아무것도 설치하지 않는다.
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

# 4. 호스트 compose.env의 pool 값이 PostgreSQL 연결 예산을 넘으면 아무것도 설치하지 않는다.
host="${tmp}/host-capacity"
make_host "${host}" 0
printf 'BOT_POSTGRES_POOL_MAX_CONNS=40\n' >>"${host}/etc-stack-secrets/hololive-bot/compose.env"
assert_rejected_without_change "${host}" "${base_repo}" "connection budget" \
  "PostgreSQL connection budget check failed"
pass "an exhausted PostgreSQL connection budget leaves /opt, prestart, units and daemon state untouched"

# 5. artifact-only 갱신은 현재 runtime bytes·mode를 유지하고 작업 트리의 미검토 설정을 복사하지 않는다.
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

# 6. 같은 revision을 다시 반영해도 기존 backup을 덮거나 보존 개수만으로 지우지 않는다.
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

echo "all sync-opt-current checks passed"
