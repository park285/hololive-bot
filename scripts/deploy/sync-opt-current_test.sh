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

umask 022
tmp="$(mktemp -d)"
trap 'find "$tmp" -type d -user "$(id -u)" ! -perm -u+w -exec chmod u+w {} +; rm -rf "$tmp"' EXIT

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
  show) [[ "$*" == 'show --all hololive-db-migrate.service -p LoadState -p ActiveState -p SubState -p Job' ]] || exit 1
    printf 'LoadState=loaded\nActiveState=inactive\nSubState=dead\nJob=\n' ;;
  is-active) exit 3 ;;
  is-enabled) exit 1 ;;
esac
EOF
cat >"${fakebin}/chown" <<'EOF'
#!/usr/bin/env bash
printf 'chown %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
EOF
cat >"${fakebin}/mv" <<'EOF'
#!/usr/bin/env bash
if [[ -f "$SYNC_TEST_EVENTS.fail-before" && "${!#}" == "$(<"$SYNC_TEST_EVENTS.fail-before")" ]]; then exit 92; fi
if [[ -f "$SYNC_TEST_EVENTS.fail-after" && "${!#}" == "$(<"$SYNC_TEST_EVENTS.fail-after")" ]]; then /usr/bin/mv "$@"; exit 93; fi
exec /usr/bin/mv "$@"
EOF
cat >"${fakebin}/sync" <<'EOF'
#!/usr/bin/env bash
active=/opt/hololive-bot/compose/app-sources/active
if [[ -f "$SYNC_TEST_EVENTS.fail-incomplete" && -f "$active" && "$(<"$active")" == "incomplete "* ]]; then exit 94; fi
exec /usr/bin/sync "$@"
EOF
cat >"${fakebin}/protected-bootstrap" <<'EOF'
#!/usr/bin/env bash
export SYNC_TEST_CONTAINER_PID=$$
exec bash /opt/hololive-bot/release-sources/tool/source/scripts/deploy/sync-opt-current.sh "$@"
EOF
cat >"${fakebin}/podman" <<'EOF'
#!/usr/bin/env bash
printf 'podman %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
case "$1:$2:${!#}" in
  container:inspect:holo-postgres|container:inspect:holo-pgbouncer)
    printf '%064d %s true healthy\n' 1 "${SYNC_TEST_CONTAINER_PID:?}" ;;
  image:inspect:*)
    id="${!#}"; id="${id#sha256:}"
    if [[ "$3" == --format && "$4" == *'{{.Os}}'* ]]; then
      case "$id" in
        "$(printf '%064d' 3)") name=hololive-api ;;
        "$(printf '%064d' 4)") name=hololive-alarm-worker ;;
        *) exit 1 ;;
      esac
      printf '%s linux/arm64 %s %s\n' "$id" "$name" "$(< /tmp/candidate-revision)"
    elif [[ "$3" == --format ]]; then
      printf '%s %s hololive-api\n' "$id" "$(< /tmp/accepted-revision)"
    else
      printf '[{"Id":"%s","Os":"linux","Architecture":"arm64","Config":{"Labels":{"org.opencontainers.image.revision":"%s"}}}]\n' "$id" "$(< /tmp/accepted-revision)"
    fi ;;
  container:inspect:hololive-api|container:inspect:hololive-alarm-worker)
    service="${!#}"; image="$(printf '%064d' 2)"; cid="$(printf '%064d' 8)"
    if [[ "$service" == hololive-alarm-worker ]]; then image="$(printf '%064d' 5)"; cid="$(printf '%064d' 9)"; fi
    if [[ "$3" == --format ]]; then
      printf '%s %s healthy\n' "$image" "$(< /tmp/accepted-revision)"
    else
      pair="/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/$service-v2-reviewed/candidate"
      printf '[{"Id":"%s","Image":"%s","State":{"Running":true,"Pid":%s,"StartedAt":"fixture"},"Config":{"Env":["HOLOLIVE_METRICS_ADDR=:30003","APP_VERSION=1.2.3"],"Labels":{"io.hololive.worker-profile-pair":"%s"}},"OCIConfigPath":"/tmp/%s.oci"}]\n' \
        "$cid" "$image" "$(<"/tmp/$service.pid")" "$pair" "$service"
    fi ;;
  exec:*:http://127.0.0.1:30003/diagnostics/workers)
    [[ "$3 $4 $5" == "/app/bin/healthcheck --body-api-key-env API_SECRET_KEY" ]] || exit 1
    printf '{"contractVersion":1,"profile":{"hash":"%s"}}\n' "$(<"/tmp/$2.loaded")" ;;
  *) exit 1 ;;
esac
EOF
chmod +x "${fakebin}"/*

make_repo() {
  local repo="$1" path
  mkdir -p "${repo}"
  for path in \
    scripts/deploy/lib/compose-env.sh scripts/deploy/quadlet-prestart.sh \
    scripts/deploy/lib/protected-binds.sh scripts/deploy/lib/sealed-app.sh \
    scripts/deploy/sync-opt-current.sh \
    scripts/deploy/quadlet-{redeploy,rollback}-service.sh \
    scripts/deploy/lib/{app-source-authority,card-migration-guard,worker-profile-pair,systemd-stop-guard}.py \
    scripts/deploy/lib/postgres-capacity.sh scripts/ci/postgres-capacity-policy.tsv \
    scripts/deploy/lib/admin-bind.sh scripts/deploy/lib/public-bind-mounts.sh \
    scripts/deploy/lib/postgres-endpoint.sh scripts/deploy/lib/source-revision.sh \
    scripts/systemd/admin-dashboard-ingress-firewall.service scripts/systemd/admin-dashboard-ingress.nft \
    verification/tla/{AlarmDispatchClaim,CollectionJobLease}/COVERAGE.md \
    hololive/hololive-api/scripts/init-db deploy; do
    mkdir -p "${repo}/$(dirname "${path}")"
    cp -R "${ROOT_DIR}/${path}" "${repo}/${path}"
  done
  local migrator="$repo/deploy/quadlet/hololive-bot/hololive-db-migrate.container"
  awk '
    /^Volume=.*:\/migrations:ro$/ || /^Environment=MIGRATION_(BASELINE_THROUGH|ALLOW_BLOCKING_INDEX_DROP)=/ {next}
    {print}
    /^Exec=--schema-only$/ {print "Volume=/opt/hololive-bot/compose/current/hololive/hololive-api/scripts/migrations:/migrations:ro"}
    /^Environment=PGAPPNAME=hololive-db-migrate$/ {
      print "Environment=MIGRATION_BASELINE_THROUGH=${MIGRATION_BASELINE_THROUGH}"
      print "Environment=MIGRATION_ALLOW_BLOCKING_INDEX_DROP=${MIGRATION_ALLOW_BLOCKING_INDEX_DROP}"
    }
    /^Environment=HOLOLIVE_MIGRATOR_USER=hololive_migrator$/ {
      print "Environment=MIGRATION_BASELINE_THROUGH="
      print "Environment=MIGRATION_ALLOW_BLOCKING_INDEX_DROP=false"
    }
  ' "$migrator" >"$repo/legacy-unit"
  mv "$repo/legacy-unit" "$migrator"
  mkdir -p "$repo/hololive/hololive-api/scripts/migrations"
  printf 'SELECT 1;\n' >"$repo/hololive/hololive-api/scripts/migrations/001_legacy.sql"
  printf '001_legacy.sql\n' >"$repo/hololive/hololive-api/scripts/migrations/manifest.txt"
  printf '\n' >>"$repo/scripts/deploy/sync-opt-current.sh"
  cat >"${repo}/scripts/deploy/verify-exec-tree-ownership.sh" <<'EOF'
#!/usr/bin/env bash
printf 'verify %s\n' "$#" >>"${SYNC_TEST_EVENTS:?}"
EOF
  git -C "${repo}" init -q
  git -C "${repo}" config tar.umask 0022
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
  case "${2:-}" in --application-only|--source-only|--protected-source) opt=opt ;; esac
  (cd "${host}" && find etc-containers-systemd usr-local-libexec etc-systemd-system etc-nftables.d etc-stack-secrets run "$opt" \
    -printf '%p %m %U:%G %i\n' -type f -printf '%p %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort)
}

run_sync() {
  local host="$1" repo="$2" opt="$1/opt"
  shift 2
  [[ ${1:-} != --source-only ]] || opt=/opt
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
    if [[ " $* " == *" --source-only "* || " $* " == *" --protected-source "* ]]; then
      jail="$host/jail"
      mkdir -p "$jail"
      mount -t tmpfs -o mode=0755 tmpfs "$jail"
      mkdir -p "$jail"/{usr,dev,etc,proc,run,sys,tmp,var,opt,repo,stubs}
      for directory in dev proc run sys; do mount --rbind "/$directory" "$jail/$directory"; done
      if [[ " $* " == *" --protected-source "* ]]; then
        for directory in containers/systemd nftables.d stack-secrets systemd/system; do
          mkdir -p "$jail/etc/$directory"
          mount --rbind "/etc/$directory" "$jail/etc/$directory"
        done
        for file in passwd group ld.so.cache; do
          touch "$jail/etc/$file"; mount --bind "/etc/$file" "$jail/etc/$file"
        done
      else
        mount --rbind /etc "$jail/etc"
      fi
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
      if [[ -e "$host/events.fail-after" ]]; then cp "$host/events.fail-after" "$jail/events.fail-after"; fi
      if [[ -e "$host/events.fail-incomplete" ]]; then touch "$jail/events.fail-incomplete"; fi
      for directory in bin sbin lib lib64; do
        [[ -e "$jail/$directory" ]] || ln -s "usr/$directory" "$jail/$directory"
      done
      if [[ " $* " == *" --protected-source "* ]]; then
        root="$jail/opt/hololive-bot"
        mkdir -p "$root"/{compose,release-sources/{tool,candidate}/source}
        chmod 0700 "$root"/release-sources/{tool,candidate}
        mount --bind "$host/opt" "$root/compose"
        mount --bind "$repo" "$root/release-sources/candidate/source"
        mount --bind "${SYNC_TEST_TOOL:?}" "$root/release-sources/tool/source"
        printf "sha256:%064d\n" 3 >"$root/release-sources/candidate/hololive-api.candidate-image"
        printf "sha256:%064d\n" 4 >"$root/release-sources/candidate/hololive-alarm-worker.candidate-image"
        cp "$host/candidate-revision" "$jail/tmp/"
        mkdir -p "$jail/docker-entrypoint-initdb.d" "$jail/etc/pgbouncer"
        mount --bind "$host/opt/current/hololive/hololive-api/scripts/init-db" "$jail/docker-entrypoint-initdb.d"
        mount -o remount,bind,ro "$jail/docker-entrypoint-initdb.d"
        mount -t tmpfs -o mode=0755 tmpfs "$jail/run/hololive-bot/postgres"
        touch "$jail/run/hololive-bot/postgres/pg_hba.conf"
        hba="$host/opt/current/deploy/postgres/pg_hba.conf"
        [[ ! -f "$host/stale-bind" ]] || hba="$host/stale-bind"
        mount --bind "$hba" "$jail/run/hololive-bot/postgres/pg_hba.conf"
        mount -o remount,bind,ro "$jail/run/hololive-bot/postgres/pg_hba.conf"
        for file in pg_hba.conf stats-users.txt pgbouncer.ini; do
          src="$host/opt/current/deploy/pgbouncer/$file"
          [[ "$file" != pgbouncer.ini ]] || src="$host/run/hololive-bot/pgbouncer/$file"
          touch "$jail/etc/pgbouncer/$file"; mount --bind "$src" "$jail/etc/pgbouncer/$file"
          mount -o remount,bind,ro "$jail/etc/pgbouncer/$file"
        done
        writers=()
        stop_writers() { for pid in "${writers[@]}"; do kill "$pid" 2>/dev/null || :; wait "$pid" 2>/dev/null || :; done; }
        trap stop_writers EXIT
        if [[ -d "$host/profile-fixture" ]]; then
          cp "$host/profile-fixture/"* "$jail/tmp/"
          for service in hololive-api hololive-alarm-worker; do
            worker="$jail/tmp/$service-root"
            base="$worker/run/hololive-bot"
            pair="$host/opt/current/artifacts/worker-profile/releases/$service-v2-reviewed/candidate"
            mkdir -p "$base"/{worker-profiles,worker-profile-release}
            mount --bind "$host/etc-stack-secrets/hololive-bot/worker-profiles" "$base/worker-profiles"
            mount --bind "$pair" "$base/worker-profile-release"
            for dir in "$base"/*; do mount -o remount,bind,ro "$dir"; done
            python3 -ISB -c "import ctypes,os,signal,sys,time; ctypes.CDLL(None).prctl(1,signal.SIGKILL,0,0,0); assert os.getppid()==int(sys.argv[3]); ready=open(sys.argv[2],\"x\"); os.chroot(sys.argv[1]); os.chdir(\"/\"); ready.write(str(os.getpid())); ready.close(); time.sleep(120)" \
              "$worker" "$jail/tmp/$service.pid" "$$" &
            writers+=("$!")
            for _ in {1..100}; do [[ ! -s "$jail/tmp/$service.pid" ]] || break; sleep 0.02; done
            [[ -s "$jail/tmp/$service.pid" ]]
          done
        fi
        chroot "$jail" env -i PATH=/stubs:/usr/bin:/usr/sbin HOME=/tmp TMPDIR=/tmp SYNC_TEST_EVENTS=/events \
          /stubs/protected-bootstrap "${@: -5}"
        exit
      fi
      mount --bind "$host/opt" "$jail/opt"
      exec chroot "$jail" env PATH=/stubs:/usr/bin:/usr/sbin HOME=/tmp TMPDIR=/tmp SYNC_TEST_EVENTS=/events REPO_ROOT=/repo OPT_ROOT=/opt bash ./bootstrap "${@: -2}"
    fi
    exec "$@"
  ' _ "${host}" "$repo" "$fakebin" "$ROOT_DIR/scripts/deploy/sync-opt-current.sh" env PATH="${fakebin}:${PATH}" SYNC_TEST_EVENTS="${host}/events" \
    REPO_ROOT="${repo}" OPT_ROOT="$opt" \
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
  [[ "${5:-}" == --protected-source ]] || {
    if compgen -G "${host}/opt/.staging-current.*" >/dev/null || compgen -G "${host}/opt/.quadlet-check.*" >/dev/null; then
      fail "${label}: temporary staging directories were left behind"
    fi
  }
}

base_repo="${tmp}/repo-base"
make_repo "${base_repo}"

host="${tmp}/host-ok"
make_host "${host}" 1
run_sync "${host}" "${base_repo}" || { cat "${host}/out" >&2; fail "valid units were rejected"; }
installed="${host}/etc-containers-systemd/hololive-bot"
for unit in "${base_repo}"/deploy/quadlet/hololive-bot/*; do
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
    -type f ! -name hololive-db-migrate.container -printf '%p %D:%i %m %U:%G %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort
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
chmod -R go-w "$source_host/opt/current/scripts"
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
fixture_acl() {
  python3 - "$1" <<'PY'
import os
import struct
import sys

mode = os.stat(sys.argv[1]).st_mode
owner, group, other = (mode >> 6) & 7, (mode >> 3) & 7, mode & 7
entries = ((1, owner, -1), (2, owner, os.getuid()), (4, group, -1), (16, group, -1), (32, other, -1))
os.setxattr(sys.argv[1], "system.posix_acl_access", struct.pack("<I", 2) + b"".join(struct.pack("<HHi", *entry) for entry in entries))
PY
}
for attack in writable-parent nonroot-owner file-link acl; do
  bad_host="$tmp/host-source-unsafe-$attack"
  cp -a "$source_host" "$bad_host"
  entry="$bad_host/opt/current/scripts/deploy/quadlet-redeploy-service.sh"
  case "$attack" in
    writable-parent) chmod g+w "${entry%/*}" ;;
    nonroot-owner) podman unshare /usr/bin/chown 1 "$entry" ;;
    file-link) ln "$entry" "$bad_host/alias" ;;
    acl) fixture_acl "$bad_host/usr-local-libexec/hololive-bot" ;;
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

generation_host="$tmp/host-generation"
generation_repo="$tmp/repo-generation"
export SYNC_TEST_TOOL="$tmp/repo-reviewed-tool"
cp -a "$source_host" "$generation_host"
mkdir "$generation_host/run/hololive-bot/postgres"
cp -a "$source_repo" "$generation_repo"
cp -a "$source_repo" "$SYNC_TEST_TOOL"
printf '\n' >>"$SYNC_TEST_TOOL/scripts/deploy/quadlet-prestart.sh"
commit_repo "$SYNC_TEST_TOOL" reviewed-prestart
tool_revision="$(git -C "$SYNC_TEST_TOOL" rev-parse HEAD)"
migrations=hololive/hololive-api/scripts/migrations
chmod 0700 "$generation_host/opt/current"
chmod g+w "$generation_host/opt/current/$migrations" "$generation_host/opt/current/$migrations/"* \
  "$generation_host/opt/current/deploy/postgres/pg_hba.conf"
mkdir -p "$generation_repo/$migrations/contracts"
rm -- "$generation_repo/$migrations/001_legacy.sql"
printf 'SELECT 2;\n' >"$generation_repo/$migrations/001_head.sql"
printf 'SELECT true;\n' >"$generation_repo/$migrations/contracts/001_head.sql"
printf '001_head.sql\n' >"$generation_repo/$migrations/manifest.txt"
awk '!/^Volume=.*:\/migrations:ro$/ && !/^Environment=MIGRATION_(BASELINE_THROUGH|ALLOW_BLOCKING_INDEX_DROP)=/' \
  "$source_repo/$migrator_path" >"$generation_repo/$migrator_path"
printf '\n' >>"$generation_repo/deploy/quadlet/README.md"
printf '\n' >>"$generation_repo/scripts/deploy/quadlet-prestart.sh"
for path in verification/tla/{AlarmDispatchClaim,CollectionJobLease}/COVERAGE.md; do printf '\n' >>"$generation_repo/$path"; done
commit_repo "$generation_repo" protected-generation
candidate_revision="$(git -C "$generation_repo" rev-parse HEAD)"
printf '%s\n' "$candidate_revision" >"$generation_host/candidate-revision"
generation_args=(--protected-source "$source_revision" "$candidate_revision" "$tool_revision" /opt/hololive-bot/release-sources/candidate/source)
assert_rejected_without_change "$generation_host" "$generation_repo" "wrong candidate" "candidate revision mismatch" \
  --protected-source "$source_revision" "$source_revision" "$tool_revision" "${generation_args[4]}"
assert_rejected_without_change "$generation_host" "$generation_repo" "wrong tool" "not the reviewed tool" \
  --protected-source "$source_revision" "$candidate_revision" "$candidate_revision" "${generation_args[4]}"
bad_repo="$tmp/repo-withdrawn-coverage"
cp -a "$generation_repo" "$bad_repo"
rm "$bad_repo/verification/tla/CollectionJobLease/COVERAGE.md"
commit_repo "$bad_repo" withdrawn-coverage
assert_rejected_without_change "$generation_host" "$bad_repo" "coverage removal" "requires an existing non-executable document" \
  --protected-source "$source_revision" "$(git -C "$bad_repo" rev-parse HEAD)" "$tool_revision" "${generation_args[4]}"
bad_repo="$tmp/repo-legacy-consumer"
cp -a "$generation_repo" "$bad_repo"
printf '#!/usr/bin/env bash\nexec /opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh "$@"\n' >"$bad_repo/scripts/deploy/quadlet-redeploy-service.sh"
commit_repo "$bad_repo" legacy-consumer
assert_rejected_without_change "$generation_host" "$bad_repo" "legacy-only coordinator" "cannot consume the reviewed sealed source" \
  --protected-source "$source_revision" "$(git -C "$bad_repo" rev-parse HEAD)" "$tool_revision" "${generation_args[4]}"
bad_repo="$tmp/repo-delegating-git"
cp -a "$generation_repo" "$bad_repo"
git -C "$bad_repo" config core.fsmonitor /unreviewed
assert_rejected_without_change "$generation_host" "$bad_repo" "Git configuration delegates to foreign code" "unsupported release clone configuration" "${generation_args[@]}"
bad_host="$tmp/host-replaced-lock"
cp -a "$generation_host" "$bad_host"
ln "$bad_host/run-lock/hololive-quadlet-redeploy.lock" "$bad_host/lock-alias"
assert_rejected_without_change "$bad_host" "$generation_repo" "aliased writer lock" "unsafe protected-source authority" "${generation_args[@]}"
bad_host="$tmp/host-wrong-candidate-image"
cp -a "$generation_host" "$bad_host"
printf '%s\n' "$source_revision" >"$bad_host/candidate-revision"
assert_rejected_without_change "$bad_host" "$generation_repo" "stale staged image" "does not match the candidate generation" "${generation_args[@]}"
cp --preserve=all "$generation_host/opt/current/deploy/postgres/pg_hba.conf" "$generation_host/stale-bind"
printf 'different mounted bytes\n' >>"$generation_host/stale-bind"
assert_rejected_without_change "$generation_host" "$generation_repo" "stale PostgreSQL bind" "running protected bind content differs" "${generation_args[@]}"
cp --preserve=all "$generation_host/opt/current/deploy/postgres/pg_hba.conf" "$generation_host/stale-bind"
bad_host="$tmp/host-withdrawal-drift"
cp -a "$generation_host" "$bad_host"
printf 'not the reviewed blob\n' >>"$bad_host/opt/current/$migrations/001_legacy.sql"
assert_rejected_without_change "$bad_host" "$generation_repo" "foreign withdrawal" "legacy archive or metadata mismatch" "${generation_args[@]}"
printf 'unreviewed\n' >"$generation_repo/untracked"
assert_rejected_without_change "$generation_host" "$generation_repo" "dirty candidate" "clean checkout" "${generation_args[@]}"
rm "$generation_repo/untracked"
for drift in mode acl; do
  bad_host="$tmp/host-generation-$drift"
  cp -a "$generation_host" "$bad_host"
  entry="$bad_host/opt/current/deploy/pgbouncer/stats-users.txt"
  if [[ "$drift" == mode ]]; then
    chmod 0600 "$entry"
    cause='protected source mode mismatch'
  else
    fixture_acl "$entry"
    cause='unsafe protected-source authority'
  fi
  assert_rejected_without_change "$bad_host" "$generation_repo" "$drift" "$cause" "${generation_args[@]}"
done
partial="$tmp/host-generation-partial"
cp -a "$generation_host" "$partial"
printf '/opt/hololive-bot/compose/app-sources/active\n' >"$partial/events.fail-before"
if run_sync "$partial" "$generation_repo" "${generation_args[@]}"; then fail "partial protected publication reported success"; fi
[[ "$(<"$partial/opt/app-sources/active")" == "incomplete $source_revision -> $candidate_revision" &&
  "$(<"$partial/opt/current/.source-revision")" == "$source_revision" ]] ||
  fail "partial protected publication retained an actionable stamp"
assert_rejected_without_change "$partial" "$generation_repo" "partial replay" "installed revision mismatch" "${generation_args[@]}"
confirmed_unknown="$tmp/host-generation-after-rename"
cp -a "$generation_host" "$confirmed_unknown"
printf '/opt/hololive-bot/compose/app-sources/active\n' >"$confirmed_unknown/events.fail-after"
status=0
run_sync "$confirmed_unknown" "$generation_repo" "${generation_args[@]}" || status=$?
[[ "$status" == 75 && "$(<"$confirmed_unknown/opt/app-sources/active")" == "$candidate_revision" ]] &&
  grep -Fq '[OUTCOME] outcome_unknown' "$confirmed_unknown/out" || fail "post-commit acknowledgement loss was misclassified"
[[ -f "$confirmed_unknown/opt/app-sources/transition-$source_revision-$candidate_revision/protected.after" ]] || fail "lost post-commit evidence"
before="$(protected_snapshot "$generation_host")"
unit="$generation_host/etc-containers-systemd/hololive-bot/${migrator_path##*/}"
installed_before="$(stat -c '%d:%i %a %u:%g %y %z' "$unit")"
old_file="$migrations/001_legacy.sql"
old_identity="$(stat -c '%d:%i' "$generation_host/opt/current/$old_file")"
run_sync "$generation_host" "$generation_repo" "${generation_args[@]}" ||
  { cat "$generation_host/out" >&2; fail "protected generation failed"; }
[[ "$(protected_snapshot "$generation_host")" == "$before" && "$(stat -c '%d:%i %a %u:%g %y %z' "$unit")" == "$installed_before" ]] ||
  fail "protected generation changed installed infrastructure"
cmp "$unit" "$source_repo/$migrator_path"
store="$generation_host/opt/app-sources"
cmp "$generation_repo/$migrator_path" "$store/$candidate_revision/$migrator_path"
if grep -Ev '^(verify [0-9]+|podman (container|image) inspect .*|systemctl show --all hololive-db-migrate.service -p LoadState -p ActiveState -p SubState -p Job)$' "$generation_host/events"; then fail "unexpected host effect"; fi
[[ "$(<"$store/active")" == "$candidate_revision" && "$(<"$store/$candidate_revision/.source-revision")" == "$candidate_revision" &&
  "$(<"$generation_host/opt/current/.source-revision")" == "$source_revision" && ! -e "$store/$candidate_revision/$old_file" ]] ||
  fail "candidate generation was not fully published"
for path in "$migrations/manifest.txt" "$migrations/001_head.sql" "$migrations/contracts/001_head.sql" deploy/quadlet/README.md; do
  cmp "$generation_repo/$path" "$store/$candidate_revision/$path"
done
receipt="$store/transition-$source_revision-$candidate_revision"
backup="$receipt/previous.actual"
cmp "$receipt/previous.metadata" "$receipt/previous.after"
cmp "$receipt/protected.before" "$receipt/protected.after"
cmp "$source_repo/$old_file" "$store/$source_revision/$old_file"
cmp "$source_repo/$old_file" "$backup/$old_file"
cmp "$source_repo/$migrations/manifest.txt" "$backup/$migrations/manifest.txt"
[[ ! -e "$backup/$migrations/001_head.sql" && "$(<"$backup/.source-revision")" == "$source_revision" &&
  "$(stat -c '%d:%i' "$generation_host/opt/current/$old_file")" == "$old_identity" &&
  "$(stat -c %a "$store/$candidate_revision")" == 500 && "$(stat -c %a "$store/$candidate_revision/$migrations/manifest.txt")" == 444 ]] ||
  fail "actual recovery point or immutable generation missing"
printf 'follow-on\n' >>"$generation_repo/deploy/quadlet/README.md"
commit_repo "$generation_repo" next-generation
next_revision="$(git -C "$generation_repo" rev-parse HEAD)"
printf '%s\n' "$next_revision" >"$generation_host/candidate-revision"
assert_rejected_without_change "$generation_host" "$generation_repo" "unaccepted previous generation" "missing accepted migrator drop-in" \
  --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}"
image="$(printf 'sha256:%064d' 2)"
dropin="$generation_host/etc-containers-systemd/hololive-bot/hololive-db-migrate.container.d"
mkdir -p "$dropin"
bash -c '. "$1"; sealed_migrator_dropin "$2" "$3"' _ "$SYNC_TEST_TOOL/scripts/deploy/lib/sealed-app.sh" \
  "/opt/hololive-bot/compose/app-sources/$candidate_revision" "$image" >"$dropin/50-migrator-image.conf"
profiles="$generation_host/opt/current/artifacts/worker-profile"
mkdir -p "$profiles/active"
mkdir -p "$generation_host/profile-fixture" "$generation_host/etc-stack-secrets/hololive-bot/worker-profiles"
printf '%s\n' "$candidate_revision" >"$generation_host/profile-fixture/accepted-revision"
for service in hololive-api hololive-alarm-worker; do
  release="$profiles/releases/$service-v2-reviewed"
  mkdir -p "$release/candidate"
  printf '/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/%s-v2-reviewed/candidate\n' "$service" >"$profiles/active/$service.pair"
  printf '%s\n' "$service" >"$release/service"
  printf '%s\n' "$candidate_revision" >"$release/candidate.revision"
  app_image="$image"
  [[ "$service" != hololive-alarm-worker ]] || app_image="$(printf 'sha256:%064d' 5)"
  printf '%s\n' "$app_image" >"$release/candidate.image"
  printf '%s\n' "$image" >"$release/migrator.image"
  printf '%064d %s\n' 12 "$image" >"$release/schema-check"
  python3 -ISB - "$SYNC_TEST_TOOL" "$generation_host" "$service" "$app_image" "$candidate_revision" <<'PY'
import json
from pathlib import Path
import runpy
import sys

tool, host, service, image, revision = sys.argv[1:]
host = Path(host)
helpers = runpy.run_path(str(Path(tool) / "scripts/deploy/lib/worker-profile-pair.py"))
pair = Path("/opt/hololive-bot/compose/current/artifacts/worker-profile/releases") / (service + "-v2-reviewed/candidate")
local = host / "opt" / pair.relative_to("/opt/hololive-bot/compose")
unit = host / "etc-containers-systemd/hololive-bot" / (service + ".container")
(local / "unit.container").write_bytes(unit.read_bytes())
(local / "profile.json").write_text(json.dumps({"fixture": service}))
identity = {"image": image, "revision": revision, "source_generation": revision, "architecture": "arm64", "os": "linux"}
if service == "hololive-api":
    identity["app_version"] = "1.2.3"
identity = helpers["save_pair"](local, service, identity)
body = helpers["dropin_contents"](pair, service, identity, identity.get("app_version"))
(local / helpers["DROPIN"]).write_bytes(body)
installed = unit.with_name(unit.name + ".d") / helpers["DROPIN"]
installed.parent.mkdir(exist_ok=True)
installed.write_bytes(body)
slot = helpers["TARGET"] + "/" + helpers["PROFILES"][service]
release_target = helpers["RELEASE_TARGET"]
desired = helpers["SOURCE"]
(host / "etc-stack-secrets/hololive-bot/worker-profiles" / helpers["PROFILES"][service]).write_bytes((local / "profile.json").read_bytes())
oci = {"process": {"env": ["STACK_WORKER_PROFILE_FILE=" + release_target + "/profile.json", "STACK_WORKER_PROFILE_SOURCE_FILE=" + slot]},
       "mounts": [{"type": "bind", "source": desired, "destination": helpers["TARGET"]},
                  {"type": "bind", "source": str(pair), "destination": release_target}]}
fixture = host / "profile-fixture"
(fixture / (service + ".oci")).write_text(json.dumps(oci))
cid = 8 if service == "hololive-api" else 9
(fixture / ("%064d.loaded" % cid)).write_text(identity["profile_hash"])
PY
done
bad_host="$tmp/host-wrong-loaded-profile"
cp -a "$generation_host" "$bad_host"
printf '%064d\n' 0 >"$bad_host/profile-fixture/$(printf '%064d.loaded' 8)"
assert_rejected_without_change "$bad_host" "$generation_repo" "unaccepted live profile" "live profile verification failed" \
  --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}"
interrupted="$tmp/host-generation-between-records"
cp -a "$generation_host" "$interrupted"
touch "$interrupted/events.fail-incomplete"
status=0
run_sync "$interrupted" "$generation_repo" --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}" || status=$?
[[ "$status" == 75 && "$(<"$interrupted/opt/app-sources/active")" == "incomplete $candidate_revision -> $next_revision" &&
  ! -e "$interrupted/opt/app-sources/transition-$candidate_revision-$next_revision" ]] || fail "old authority survived follow-on invalidation"
saved=("$interrupted"/opt/.quadlet-check.*/transition/previous.actual/.source-revision)
[[ "${#saved[@]}" == 1 && "$(<"${saved[0]}")" == "$candidate_revision" ]] || fail "unpublished transition lost its recovery artifact"
run_sync "$generation_host" "$generation_repo" --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}" ||
  { cat "$generation_host/out" >&2; fail "sealed predecessor was not admitted"; }
[[ "$(<"$store/active")" == "$next_revision" && "$(<"$generation_host/opt/current/.source-revision")" == "$source_revision" ]] ||
  fail "follow-on publication changed the runtime root"
pass "protected generations pin A/M/D archives without replacing runtime or bound inodes and retain verified predecessors"

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
