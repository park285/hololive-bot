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
esac
EOF
cat >"${fakebin}/chown" <<'EOF'
#!/usr/bin/env bash
printf 'chown %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
EOF
chmod +x "${fakebin}"/*

make_repo() {
  local repo="$1" path
  mkdir -p "${repo}"
  for path in \
    scripts/deploy/lib/compose-env.sh scripts/deploy/quadlet-prestart.sh \
    scripts/deploy/sync-opt-current.sh \
    scripts/deploy/lib/postgres-capacity.sh scripts/ci/postgres-capacity-policy.tsv \
    scripts/deploy/lib/admin-bind.sh scripts/deploy/lib/public-bind-mounts.sh \
    scripts/deploy/lib/postgres-endpoint.sh scripts/deploy/lib/source-revision.sh \
    scripts/systemd/admin-dashboard-ingress-firewall.service scripts/systemd/admin-dashboard-ingress.nft \
    hololive/hololive-api/scripts/init-db deploy; do
    mkdir -p "${repo}/$(dirname "${path}")"
    cp -R "${ROOT_DIR}/${path}" "${repo}/${path}"
  done
  printf '\n' >>"$repo/scripts/deploy/sync-opt-current.sh"
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

make_host() {
  local host="$1" x_spaces="$2" installed="${1}/etc-containers-systemd/hololive-bot"
  mkdir -p "${host}"/{etc-containers-systemd,usr-local-libexec,etc-stack-secrets/hololive-bot,etc-systemd-system,etc-nftables.d,run-lock}
  mkdir -p "${host}/run/hololive-bot/pgbouncer" "${host}/run/lock"
  mkdir -m 0700 "${host}/run/mount"
  printf 'accepted pool configuration\n' >"${host}/run/hololive-bot/pgbouncer/pgbouncer.ini"
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
  local host="$1" opt=opt/current
  case "${2:-}" in --application-only|--source-only) opt=opt ;; esac
  (cd "${host}" && find etc-containers-systemd usr-local-libexec etc-systemd-system etc-nftables.d etc-stack-secrets run "$opt" \
    -printf '%p %m %U:%G %i\n' -type f -printf '%p %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort)
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
    mount --bind "${host}/run" /run
    mount --bind "${host}/run-lock" /run/lock
    exec "$@"
  ' _ "${host}" env PATH="${fakebin}:${PATH}" SYNC_TEST_EVENTS="${host}/events" \
    REPO_ROOT="${repo}" OPT_ROOT="${host}/opt" \
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
for lib in admin-bind.sh public-bind-mounts.sh postgres-capacity.sh compose-env.sh postgres-endpoint.sh; do
  cmp -s "${base_repo}/scripts/deploy/lib/${lib}" "${host}/usr-local-libexec/hololive-bot/lib/${lib}" ||
    fail "prestart dependency not installed: ${lib}"
done
bash -c 'source "$1"; source "$2"; postgres_endpoint_quadlet "$3" "$4"' _ \
  "${host}/usr-local-libexec/hololive-bot/lib/compose-env.sh" \
  "${host}/usr-local-libexec/hololive-bot/lib/postgres-endpoint.sh" \
  "${installed}/hololive-api.container" "${host}/etc-stack-secrets/hololive-bot/compose.env" >/dev/null ||
  fail "installed endpoint library cannot resolve the effective application endpoint"
bash -c 'source "$1"; postgres_capacity_write_pgbouncer_config "$2" "$3" "$4"' _ \
  "${host}/usr-local-libexec/hololive-bot/lib/postgres-capacity.sh" \
  "${host}/opt/current/scripts/ci/postgres-capacity-policy.tsv" \
  "${host}/etc-stack-secrets/hololive-bot/compose.env" "${host}/pgbouncer.ini" ||
  fail "installed capacity library cannot generate the pooler configuration"
[[ -s "${host}/pgbouncer.ini" && "$(stat -c '%a' "${host}/pgbouncer.ini")" == 644 ]] ||
  fail "pooler configuration is missing or unreadable by the container"
for config in pg_hba.conf stats-users.txt; do
  cmp -s "${base_repo}/deploy/pgbouncer/${config}" "${host}/opt/current/deploy/pgbouncer/${config}" ||
    fail "pooler configuration not installed: ${config}"
done
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
sed -e 's/^Exec=--schema-only$/Exec=--pool-only=auth/' \
  -e 's/ hololive-db-migrate schema$/ hololive-db-migrate pool-auth/' \
  "${base_repo}/${migrator_path}" >"${w1_repo}/${migrator_path}"
commit_repo "${w1_repo}" pool-window
w1_host="${tmp}/host-pool"
make_host "${w1_host}" 1
run_sync "${w1_host}" "${w1_repo}" || { cat "${w1_host}/out" >&2; fail "pool-window installation failed"; }
for path in data logs backups runtime-config artifacts/worker-profile; do
  mkdir -p "${w1_host}/opt/current/${path}"
  printf 'accepted runtime\n' >"${w1_host}/opt/current/${path}/accepted"
done
pool_dropin="${w1_host}/etc-containers-systemd/hololive-bot/holo-pgbouncer.container.d"
mkdir -p "$pool_dropin"
printf '[Container]\nImage=sha256:%064d\n' 1 >"$pool_dropin/50-pooler-image.conf"
touch -d @1 "${w1_host}/opt/current/deploy/postgres/pg_hba.conf" \
  "${w1_host}/opt/current/deploy/pgbouncer/"* "${w1_host}/opt/current/hololive/hololive-api/scripts/init-db/"*

protected_snapshot() (
  cd "$1"
  find etc-containers-systemd usr-local-libexec etc-systemd-system etc-nftables.d etc-stack-secrets run/hololive-bot \
    opt/current/{deploy/{pgbouncer,postgres,nginx,topology},hololive/hololive-api/scripts/init-db,data,logs,backups,runtime-config,artifacts/worker-profile} \
    -type f ! -name hololive-db-migrate.container -printf '%p %i %m %U:%G %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort
)
w2_repo="${tmp}/repo-card"
cp -a "${base_repo}" "$w2_repo"
printf 'card release\n' >"$w2_repo/hololive/hololive-api/VERSION"
commit_repo "$w2_repo" card-window
host="${tmp}/host-card"
cp -a "$w1_host" "$host"
before="$(protected_snapshot "$host")"
run_sync "$host" "$w2_repo" --application-only || { cat "$host/out" >&2; fail "card-window publication failed"; }
[[ "$(protected_snapshot "$host")" == "$before" ]] || fail "card window changed a protected inode, mode or content"
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
assert_rejected_without_change "$bad_host" "$source_repo" "unreviewed precursor bytes" "tree drift" --source-only "$installed_revision"
bad_repo="$tmp/repo-source-deleted"
cp -a "$source_repo" "$bad_repo"
rm "$bad_repo/hololive/hololive-api/VERSION"
commit_repo "$bad_repo" remove-source
assert_rejected_without_change "$source_host" "$bad_repo" "removed source" "regular-file modifications" --source-only "$installed_revision"
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

for path in opt/current/deploy/postgres/pg_hba.conf opt/current/deploy/pgbouncer/stats-users.txt \
    opt/current/hololive/hololive-api/scripts/init-db/00-assert-pg18-runtime.sql \
    etc-containers-systemd/hololive-bot/{hololive-api,holo-pgbouncer,hololive-alarm-worker}.container \
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
printf '[Container]\nExec=--pool-only=auth\n' >"$dropin/60-command.conf"
assert_rejected_without_change "$host" "$w2_repo" "migrator override" "requires the schema-only migrator" --application-only
assert_rejected_without_change "$w1_host" "$w1_repo" "pool candidate" "requires the schema-only migrator" --application-only
drift_repo="${tmp}/repo-infra-change"
cp -a "$w2_repo" "$drift_repo"
printf 'drift\n' >"$drift_repo/deploy/pgbouncer/unreviewed.conf"
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
  exec bash "$2/scripts/deploy/lib/po-central-remote.sh" c deploy \
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
