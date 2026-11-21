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

case "$#:${1:-}" in
  0:) released=false; bash "$0" --unstarted-source ;;
  1:--released-predecessor) released=true ;;
  1:--unstarted-source) released=false ;;
  *) fail "usage: $0 [--released-predecessor|--unstarted-source]" ;;
esac

[[ -x "${QUADLET_BIN}" ]] || fail "quadlet generator missing: ${QUADLET_BIN}"
unshare --user --map-root-user --mount true 2>/dev/null || fail "unprivileged user+mount namespace unavailable"

umask 022
tmp="$(mktemp -d)"
trap 'find "$tmp" -type d -user "$(id -u)" ! -perm -u+w -exec chmod u+w {} +; rm -rf "$tmp"' EXIT
export SYNC_TEST_SYSTEMD_ETC="$tmp/etc-systemd"
mkdir -p "$SYNC_TEST_SYSTEMD_ETC/system"

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
if [[ $* == *--exchange* && ${*: -2:1} == */incomplete && -f "$SYNC_TEST_EVENTS.first-exchange" ]]; then
  active=${!#}
  cp "$active" "$active.restored"
  /usr/bin/mv -T "$active.restored" "$active"
fi
if [[ $* == *--exchange* && ${*: -2:1} == */active && -f "$SYNC_TEST_EVENTS.exchange-race" ]]; then
  active=${!#}
  cp "$SYNC_TEST_EVENTS.exchange-race" "$active.restored"
  /usr/bin/mv -T "$active.restored" "$active"
fi
if [[ -f "$SYNC_TEST_EVENTS.fail-before" && "${!#}" == "$(<"$SYNC_TEST_EVENTS.fail-before")" ]]; then exit 92; fi
if [[ -f "$SYNC_TEST_EVENTS.fail-after" && "${!#}" == "$(<"$SYNC_TEST_EVENTS.fail-after")" ]]; then /usr/bin/mv "$@"; exit 93; fi
exec /usr/bin/mv "$@"
EOF
cat >"${fakebin}/sync" <<'EOF'
#!/usr/bin/env bash
active=/opt/hololive-bot/compose/app-sources/active
if [[ -f "$SYNC_TEST_EVENTS.final-writer" && -f "$active" ]]; then
  read -r state base arrow revision <"$active"
  if [[ $state == incomplete && $arrow == '->' && -s ${active%/*}/transition-$base-$revision/pairs.after ]]; then
    touch "$SYNC_TEST_EVENTS.writer-unhealthy"
    echo 'injected final writer health change' >>"$SYNC_TEST_EVENTS"
  fi
fi
if [[ -f "$SYNC_TEST_EVENTS.interrupt" && -f "$active" && "$(<"$active")" == "incomplete "* ]]; then
  signal=$(<"$SYNC_TEST_EVENTS.interrupt")
  if [[ $signal != cleanup-* ]]; then
    case "$signal" in
      full-pipe)
        python3 -ISB -c 'import os
fd=os.open("/tmp/cleanup-pipe", os.O_WRONLY|os.O_NONBLOCK)
for size in (4096,1):
    try:
        while True: os.write(fd,b"x"*size)
    except BlockingIOError: pass' ;;
      file-limit) truncate -s 33554432 /tmp/limit-stderr ;;
      sync-error) ;;
      *) kill -s "$signal" "$PPID" ;;
    esac
    exit 1
  fi
fi
if [[ -f "$SYNC_TEST_EVENTS.fail-incomplete" && -f "$active" && "$(<"$active")" == "incomplete "* ]]; then exit 94; fi
exec /usr/bin/sync "$@"
EOF
cat >"${fakebin}/rm" <<'EOF'
#!/usr/bin/env bash
if [[ -f /events.interrupt && $1 == -rf && ${2:-} == /opt/hololive-bot/compose/.staging-current.* ]]; then
  case "$(< /events.interrupt)" in
    cleanup-term) kill -TERM "$PPID"; exit 1 ;;
    cleanup-error) exit 1 ;;
  esac
fi
exec /usr/bin/rm "$@"
EOF
cat >"${fakebin}/find" <<'EOF'
#!/usr/bin/env bash
if [[ -f /tamper-staged-helper && "$1" == /opt/hololive-bot/compose/.staging-current.* && "$2" == -type && "$3" == f && "$#" == 3 ]]; then
  printf '\n' >>"$1/scripts/deploy/lib/postgres-endpoint.sh" || exit 1
fi
exec /usr/bin/find "$@"
EOF
cat >"${fakebin}/protected-bootstrap" <<'EOF'
#!/usr/bin/env bash
export SYNC_TEST_CONTAINER_PID=$$
if [[ -f /events.interrupt ]]; then
  case "$(</events.interrupt)" in
    full-pipe|file-limit)
      exec python3 -ISB -c 'import fcntl, os, resource, subprocess, sys
from pathlib import Path
if Path("/events.interrupt").read_text().strip() == "full-pipe":
    os.mkfifo("/tmp/cleanup-pipe")
    writer = os.open("/tmp/cleanup-pipe",os.O_RDWR)
else:
    limit = 32*1024*1024
    writer = os.open("/tmp/limit-stderr", os.O_CREAT|os.O_WRONLY|os.O_APPEND, 0o600)
    resource.setrlimit(resource.RLIMIT_FSIZE,(limit,limit))
p=subprocess.run(["bash","/opt/hololive-bot/release-sources/tool/source/scripts/deploy/sync-opt-current.sh",*sys.argv[1:]],stderr=writer,timeout=90)
for name in ("/opt","/run/lock/hololive-quadlet-redeploy.lock"):
    fd=os.open(name,os.O_RDONLY)
    fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
sys.exit(p.returncode)' "$@" ;;
    *) exec 2>&- ;;
  esac
fi
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
      health=healthy
      [[ ! -e $SYNC_TEST_EVENTS.writer-unhealthy || $service != hololive-alarm-worker ]] || health=unhealthy
      printf '%s %s %s\n' "$image" "$(< /tmp/accepted-revision)" "$health"
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

slot_table_files=(scripts/deploy/lib/collector-slots.sh scripts/deploy/lib/collector-slots.tsv)
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
    hololive/hololive-api/scripts/init-db deploy "${slot_table_files[@]}"; do
    mkdir -p "${repo}/$(dirname "${path}")"
    cp -R "${ROOT_DIR}/${path}" "${repo}/${path}"
  done
  local migrator="$repo/deploy/quadlet/hololive-bot/hololive-db-migrate.container"
  printf '\n' >>"$repo/scripts/deploy/lib/postgres-endpoint.sh"
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
printf 'verify %s\n' "$#" >>"${SYNC_TEST_EVENTS:-/events}"
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
  local -a namespaces=(--user --map-root-user --mount)
  [[ ${1:-} != --unstarted-source ]] || namespaces+=(--uts)
  # shellcheck disable=SC2016 # namespace 안의 bash가 확장한다.
  unshare "${namespaces[@]}" bash -c '
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
    if [[ " $* " == *" --source-only "* || " $* " == *" --protected-source "* || "${!#}" == --unstarted-source ]]; then
      jail="$host/jail"
      mkdir -p "$jail"
      mount -t tmpfs -o mode=0755 tmpfs "$jail"
      mkdir -p "$jail"/{usr,dev,etc,proc,run,sys,tmp,var,opt,repo,stubs}
      for directory in dev proc run sys; do mount --rbind "/$directory" "$jail/$directory"; done
      if [[ " $* " == *" --protected-source "* || "${!#}" == --unstarted-source ]]; then
        mkdir "$jail/etc/systemd"
        mount --bind "${SYNC_TEST_SYSTEMD_ETC:?}" "$jail/etc/systemd"
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
      for directory in bin sbin lib lib64 share libexec local/lib local/libexec; do
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
      if [[ -e "$host/events.final-writer" ]]; then touch "$jail/events.final-writer"; fi
      if [[ -e "$host/events.first-exchange" ]]; then touch "$jail/events.first-exchange"; fi
      if [[ -e "$host/events.exchange-race" ]]; then cp "$host/events.exchange-race" "$jail/events.exchange-race"; fi
      if [[ -e "$host/events.fail-before" ]]; then cp "$host/events.fail-before" "$jail/events.fail-before"; fi
      if [[ -e "$host/events.fail-after" ]]; then cp "$host/events.fail-after" "$jail/events.fail-after"; fi
      if [[ -e "$host/events.interrupt" ]]; then cp "$host/events.interrupt" "$jail/events.interrupt"; fi
      if [[ -e "$host/events.fail-incomplete" ]]; then touch "$jail/events.fail-incomplete"; fi
      if [[ -e "$host/tamper-staged-helper" ]]; then touch "$jail/tamper-staged-helper"; fi
      for directory in bin sbin lib lib64; do
        [[ -e "$jail/$directory" ]] || ln -s "usr/$directory" "$jail/$directory"
      done
      if [[ " $* " == *" --protected-source "* || "${!#}" == --unstarted-source ]]; then
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
            pointer="$(<"$host/opt/current/artifacts/worker-profile/active/$service.pair")"
            pair="$host/opt/${pointer#/opt/hololive-bot/compose/}"
            mkdir -p "$base"/{worker-profiles,worker-profile-release}
            mount --bind "$host/etc-stack-secrets/hololive-bot/worker-profiles" "$base/worker-profiles"
            mount --bind "$pair" "$base/worker-profile-release"
            for dir in "$base"/*; do mount -o remount,bind,ro "$dir"; done
            python3 -ISB -c "import ctypes,os,signal,sys; ctypes.CDLL(None).prctl(1,signal.SIGKILL,0,0,0); assert os.getppid()==int(sys.argv[3]); ready=open(sys.argv[2],\"x\"); os.chroot(sys.argv[1]); os.chdir(\"/\"); ready.write(str(os.getpid())); ready.close(); signal.pause()" \
              "$worker" "$jail/tmp/$service.pid" "$$" &
            writers+=("$!")
            for _ in {1..100}; do [[ ! -s "$jail/tmp/$service.pid" ]] || break; sleep 0.02; done
            [[ -s "$jail/tmp/$service.pid" ]]
          done
        fi
        if [[ "${!#}" == --unstarted-source ]]; then
          /usr/bin/hostname hololive-seoul
          candidate="$(<"$host/candidate-revision")"
          for which in tool holo-api holo-worker; do
            dst="$root/release-sources/dv-$which-$candidate"
            mkdir -m0700 "$dst"
            mkdir "$dst/source"
            case "$which" in
              tool) clone=$SYNC_TEST_TOOL ;;
              holo-api) clone=$repo ;;
              holo-worker) clone=$host/worker-source ;;
            esac
            mount --bind "$clone" "$dst/source"
            cp "$host/staged.bundle" "$dst/source.bundle"
          done
          source="$(<"$host/profile-fixture/source-revision")"
          prior="$root/release-sources/dv-holo-api-$source"
          mkdir -m0700 "$prior"
          mkdir "$prior/source"
          mount --bind "$host/prior-source" "$prior/source"
          prior_tool="$root/release-sources/dv-tool-$source"
          mkdir -m0700 "$prior_tool"
          mkdir "$prior_tool/source"
          mount --bind "$host/prior-tool" "$prior_tool/source"
          mkdir "$jail/case"
          follow="$root/release-sources/follow"
          mkdir -m0700 "$follow"
          mkdir "$follow/source"
          mount --bind "$host/follow-source" "$follow/source"
          mkdir -m0700 "$root/release-sources/follow-tool"
          mkdir "$root/release-sources/follow-tool/source"
          mount --bind "$host/follow-tool" "$root/release-sources/follow-tool/source"
          cp "$(dirname "$bootstrap")"/unstarted-source-case* "$jail/case/"
          cp "$host/../transport-command" "$host/../bootstrap-command" "$jail/case/"
          cp "$(command -v uv)" "$jail/case/uv"
          sha256sum "$(dirname "$bootstrap")/supersession-window.py" | cut -d" " -f1 >"$jail/case/approved-supervisor"
          chmod 0755 "$jail/case/unstarted-source-case-host.py"
          cp "$(dirname "$bootstrap")/testdata/ingress-firewall.json" "$jail/case/"
          if [[ -n "${HOLOLIVE_TEST_841_OBSERVER:-}" ]]; then
            cp "$HOLOLIVE_TEST_841_OBSERVER" "$jail/case/activate-holo.py"
            (cd "$jail/case" && echo "276262a772edcd8296386df2b9524afe875f8789282293106b12602bf1a383d1  activate-holo.py" | sha256sum -c --status)
            mkdir -p "$jail/usr/local/bin"
            for cmd in nft tailscale ip; do ln -s /case/unstarted-source-case-host.py "$jail/usr/local/bin/$cmd"; done
          fi
          cp /usr/bin/sync "$jail/case/kernel-sync"
          for cmd in systemctl podman journalctl busctl systemd-run sync; do
            mount --bind "$jail/case/unstarted-source-case-host.py" "$jail/usr/bin/$cmd"
          done
          chroot "$jail" env -i PATH=/usr/bin:/usr/sbin:/stubs HOME=/tmp TMPDIR=/tmp SYNC_TEST_EVENTS=/events \
            bash /case/unstarted-source-case.sh
          exit
        fi
        chroot "$jail" env -i PATH=/stubs:/usr/bin:/usr/sbin HOME=/tmp TMPDIR=/tmp SYNC_TEST_EVENTS=/events \
          /stubs/protected-bootstrap "${@: -5}"
        exit
      fi
      mount --bind "$host/opt" "$jail/opt"
      if [[ -f "$host/foreign-owner-fixture" ]]; then
        entry="$jail/opt/current/scripts/deploy/quadlet-redeploy-service.sh"
        mount --bind /usr/bin/true "$entry"
        [[ $(stat -c %u "$entry") != 0 ]]
      fi
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

if [[ ${1:-} == --unstarted-source ]]; then
  delta_base=d92cf34bcc24715e60edfc4de5c2fabe5175b7db
  changed=(CHANGELOG.md VERSION deploy/quadlet/README.md hololive/{hololive-api,hololive-alarm-worker}/VERSION
    scripts/deploy/{lib/sealed-app.sh,lib/unstarted-source.py,quadlet-redeploy-service.sh,supersession-window.py,sync-opt-current.sh,sync-opt-current_test.sh}
    scripts/deploy/unstarted-source-case{.sh,-evidence.py,-host.py,-pairs.py})
  prepare_recovery_checkout() {
    local repo=$1 dest=$2 head recovery path
    head=$(git -C "$repo" rev-parse HEAD)
    recovery=$(git -C "$repo" rev-list --first-parent --reverse "$delta_base..$head" | sed -n '1p')
    [[ -n $recovery && $(git -C "$repo" show -s --format=%P "$recovery") == "$delta_base" ]] || fail "missing original recovery child"
    bash -eu -o pipefail -c '. "$1"; sealed_supersession_delta "$2" "$3"' _ "$ROOT_DIR/scripts/deploy/lib/sealed-app.sh" "$repo" "$recovery" ||
      fail "original recovery child is not the approved supersession delta"
    git clone -q --shared --no-checkout --no-tags "$repo" "$dest"
    git -C "$dest" checkout -q --detach "$recovery"
    if [[ $head == "$recovery" ]]; then
      for path in "${changed[@]}"; do cp -p "$repo/$path" "$dest/$path"; done
    fi
  }
  invoking_checkout=$ROOT_DIR
  prepare_recovery_checkout "$ROOT_DIR" "$tmp/recovery-checkout"
  git clone -q --shared "$tmp/recovery-checkout" "$tmp/later"
  printf 'unrelated later edit\n' >>"$tmp/later/docs/current/PROJECT_MAP.md"
  prepare_recovery_checkout "$tmp/later" "$tmp/unrelated"
  git -C "$tmp/unrelated" diff --exit-code HEAD -- docs/current/PROJECT_MAP.md
  commit_repo "$tmp/later" unrelated
  printf 'later approved-path edit\n' >>"$tmp/later/deploy/quadlet/README.md"
  commit_repo "$tmp/later" later
  prepare_recovery_checkout "$tmp/later" "$tmp/pinned"
  [[ $(git -C "$tmp/pinned" rev-parse HEAD) == "$(git -C "$tmp/recovery-checkout" rev-parse HEAD)" ]]
  git -C "$tmp/pinned" diff --exit-code HEAD
  git clone -q --shared "$tmp/recovery-checkout" "$tmp/misordered"
  git -C "$tmp/misordered" checkout -q --detach "$delta_base"
  printf 'unrelated first child\n' >>"$tmp/misordered/docs/current/PROJECT_MAP.md"
  commit_repo "$tmp/misordered" unrelated-first
  if (prepare_recovery_checkout "$tmp/misordered" "$tmp/misordered-out") >"$tmp/misordered.log" 2>&1; then
    fail 'unrelated first child of the recovery base was accepted'
  fi
  grep -Fq 'original recovery child is not the approved supersession delta' "$tmp/misordered.log" || fail 'unrelated recovery child refused for another reason'
  ROOT_DIR="$tmp/recovery-checkout"
  slot_table_files=()
fi
base_repo="${tmp}/repo-base"
make_repo "${base_repo}"

if [[ "${1:-}" == --unstarted-source ]]; then
  UV_CACHE_DIR="$tmp/uv" uv run --no-project python -ISB - "$ROOT_DIR" <<'PYTHON'
import os
from pathlib import Path
import runpy
import subprocess
import sys
import time

deploy = Path(sys.argv[1]) / 'scripts/deploy'
proof = runpy.run_path(str(deploy / 'lib/unstarted-source.py'))
production_case = ('6d7e8749983723867fa52d5a0e9cd648198d2e61', 'hololive-api-v2-20261119T121019Z-lVIJBA')
original_files = [
    ['activation.config-before', '827c115adbde3114e0460ae39100ce47df96627f9841279b7f12a40d24af800b', 92875647, 1795090207646973883, 1795090207646973883],
    ['activation.requested', '015038b6cafb5bf1c4f21758ddac40296fa781fe47ceb449de5d5fbbf1a58453', 92881408, 1795090210830331463, 1795090210830331463],
    ['coordinator.diagnostics', '1c684a3bfe89f1607f97cd425e088fe90d2ff9f77a9b055b8f99d0fd221f6b87', 92881409, 1795090226981817260, 1795090226981817260],
    ['coordinator.exit', '9cfb3710b6a4d46a99930bed1514f3a1a0df882d28aa9265135fa424f2b5264d', 92881410, 1795090226985217801, 1795090226985217801],
    ['activation.not-accepted', '955a9a7fd1fd2549d284a61ae56bb12033ad06091f6b7bfbc3ef60d63ff8cb13', 92881411, 1795090227020290738, 1795090227020290738],
]
proof['original_receipt_pin'](*production_case, original_files)
for column in (2,3,4):
    changed = [row.copy() for row in original_files]
    changed[2][column] += 1
    try:
        proof['original_receipt_pin'](*production_case, changed)
    except proof['Refusal']:
        pass
    else:
        sys.exit('restored production receipt admitted')
expired = time.time_ns() // 1000 - 30_000_001
try:
    proof['fresh_confirmation']({'cursor':f'b=boot;s=seq;i=1;t={expired:x}'})
except proof['Refusal']:
    pass
else:
    sys.exit('expired confirmation cursor admitted')
timestamps = [1795090207646973882,1795090207646973883,1795090210830331463,1795090226981817260,1795090226985217801,1795090227020290738]
start = 1795090210829668459
proof['original_window'](start,timestamps)
tick = round(time.clock_getres(5)*10**9)
timestamps[2] = start - tick
proof['original_window'](start,timestamps)
timestamps[2] -= 1
try:
    proof['original_window'](start,timestamps)
except proof['Refusal']:
    pass
else:
    sys.exit('original request outside inode clock resolution admitted')
os.dup2(os.open('/dev/null', os.O_RDONLY), 19)
invoke = runpy.run_path(str(deploy / 'supersession-window.py'))['invoke']
messages = []
def dispatch(action, witness, remaining, alive, send):
    alive()
    assert action == 'schema' and witness == {'cursor': 'fixture'} and remaining > 0
    return 'received'
assert invoke(['/usr/bin/python3','-ISB',str(deploy / 'lib/unstarted-source.py'),'--relay','schema','{"cursor":"fixture"}'],
              messages.append, timeout=5, dispatch=dispatch) == 0 and messages == ['received']
for script in ('printf partial; sleep 8', 'exec 1>&- 2>&-; sleep 8'):
    try:
        invoke(['/usr/bin/bash','-c',script], lambda line: None, timeout=.2)
    except (TimeoutError, subprocess.TimeoutExpired):
        continue
    sys.exit('hung coordinator admitted')

import signal
import select
import tempfile
from unittest.mock import patch
supervisor = runpy.run_path(str(deploy / 'supersession-window.py'))
with tempfile.TemporaryDirectory() as tmp:
    control = Path(tmp)
    sender = """import os,sys,time
from pathlib import Path
p=Path(sys.argv[1])
(p/'verified').write_text(str(os.getpid()))
while not (p/'release').exists():
    time.sleep(.005)
(p/'enqueued').touch()
"""
    owner = os.fork()
    if not owner:
        supervisor['manager_send']('/usr/bin/python3','-ISB','-c',sender,str(control))
        os._exit(0)
    deadline = time.monotonic() + 10
    while not (control/'verified').is_file() or not (control/'verified').stat().st_size:
        if time.monotonic() >= deadline:
            sys.exit('manager sender did not reach dispatch barrier')
        time.sleep(.005)
    sender_fd = os.pidfd_open(int((control/'verified').read_text()))
    os.kill(owner, signal.SIGKILL)
    os.waitpid(owner,0)
    (control/'release').touch()
    assert select.select([sender_fd],[],[],5)[0] and not (control/'enqueued').exists(), 'orphan manager request'
    os.close(sender_fd)

queued = []
real_select = select.select
def exit_before_packet(readers, writers, errors, timeout):
    pidfds = [fd for fd in readers if isinstance(fd,int)]
    if pidfds:
        assert real_select(pidfds, [], [], 5)[0], 'coordinator did not exit with packet outstanding'
    return real_select(readers, writers, errors, timeout)
with patch.object(select,'select',exit_before_packet):
    try:
        invoke(['/usr/bin/python3','-ISB','-c',
                'import os,signal; os.write(int(os.environ["HOLOLIVE_MANAGER_FD"]),__import__("json").dumps(["schema",{}]).encode()); os.kill(os.getpid(),signal.SIGKILL)'],
               lambda _:None, timeout=10, dispatch=lambda *args:queued.append(args))
    except ValueError:
        pass
assert not queued, 'dispatched packet after coordinator death'

clock = [100_000_000]
tips = iter(('first','tail'))
segments = []
def scan(begin, end, *args, **kwargs):
    segments.append((begin,end))
    clock[0] += 45_000_000_000
with patch.dict(proof['journal'].__globals__, dict(manager_rate_limit=lambda *_: (0,0), journald_generation=lambda: ('a'*32, 1), run=lambda *a: 'journald' if a[0]=='podman' else '',
        journal_rows=lambda _: [dict(__REALTIME_TIMESTAMP='1',__CURSOR='anchor')], journal_tip=lambda:next(tips),
        cursor_fields=lambda _: ('','',0,100), journal_segment=scan)), patch.object(time,'time_ns',lambda:clock[0]):
    proof['journal'](2_000,90_000)
assert segments == [('anchor','first'),('first','tail')], 'historical scan did not bridge to a fresh cursor'
PYTHON
  UV_CACHE_DIR="$tmp/uv" uv run --no-project python -ISB - "$ROOT_DIR/deploy/quadlet/README.md" "$tmp/transport-command" "$tmp/bootstrap-command" <<'PYTHON'
from pathlib import Path
import re
import sys

blocks = re.findall(r"^```bash\n(.*?)^```", Path(sys.argv[1]).read_text(), re.M | re.S)
for path, marker in zip(sys.argv[2:], ('"${ssh_window[@]}"', 'ssh_window=(ssh -T'), strict=True):
    example, = [block for block in blocks if marker in block]
    Path(path).write_text(example)
PYTHON
  (
    export UV_CACHE_DIR="$tmp/uv"
    export OLD NEW RUNNING FAILED_API_RELEASE API_MANIFEST WORKER_MANIFEST
    printf -v OLD '%040d' 1; printf -v NEW '%040d' 2; printf -v RUNNING '%040d' 3
    FAILED_API_RELEASE=hololive-api-v2-20261119T121019Z-lVIJBA
    printf -v API_MANIFEST '%064d' 4; printf -v WORKER_MANIFEST '%064d' 5
    response=$(uv run --no-project python -c 'import json,os; e=os.environ
print(json.dumps(dict(phase="source",identity=dict(old=e["OLD"],new=e["NEW"],running=e["RUNNING"],failed=e["FAILED_API_RELEASE"],manifests=[e["API_MANIFEST"],e["WORKER_MANIFEST"]],boot="af2e24c9-9efd-4ee5-bb99-66394247d9f4"),window_sha256="2"*64,accepted_sha256="3"*64,snapshot_sha256="4"*64,window_nonce="5"*32,output_nonce="6"*32),sort_keys=True,separators=(",",":")))') || exit 1
    documented_window() {
      input=/dev/null phase=source evidence="$evidence" bash -c 'cmd=$1; shift; ssh_window=("$@"); . "$cmd"' _ "$tmp/transport-command" "$@"
    }
    evidence=$tmp/operator
    mkdir "$evidence" || exit 1
    documented_window printf '%s\n' "$response" || exit 1
    [[ -s $evidence/source/transport && $(<"$evidence/source/process_exit") == 0 ]] || exit 1
    mkdir "$tmp/unverified-receipt" || exit 1
    printf 'worker transport\n' >"$tmp/worker-original" || exit 1
    worker_hash=$(sha256sum "$tmp/worker-original") || exit 1
    receipt_response=$(printf '{"confirmed":"worker","output_nonce":"%032d","worker_transport_sha256":"%s"}' 1 "${worker_hash%% *}") || exit 1
    rc=0
    input="$tmp/worker-original" phase=receipt evidence="$tmp/unverified-receipt" bash -c '
      ssh_window=(printf "%s\n" "$2")
      remote_command() { return 1; }
      ssh() { exit 99; }
      . "$1"
    ' _ "$tmp/transport-command" "$receipt_response" || rc=$?
    [[ $rc == 75 && ! -e "$tmp/unverified-receipt/receipt/success" ]] || exit 1
    for bad in interrupted whitespace extra-newline missing wrong-new partial; do
      evidence=$tmp/operator-$bad
      mkdir "$evidence" || exit 1
      rc=0
      case $bad in
        interrupted) documented_window bash -c 'printf "%s\n" "$1"; exit 255' _ "$response" || rc=$? ;;
        whitespace) documented_window printf ' %s\n' "$response" || rc=$? ;;
        extra-newline) documented_window printf '%s\n\n' "$response" || rc=$? ;;
        missing) documented_window printf '{"phase":"source"}\n' || rc=$? ;;
        wrong-new) documented_window printf '%s\n' "${response/$NEW/$RUNNING}" || rc=$? ;;
        partial)
          mkdir "$evidence/source" || exit 1
          printf '0\n' >"$evidence/source/process_exit" || exit 1
          documented_window touch "$evidence/ssh-ran" || rc=$?
          [[ ! -e $evidence/ssh-ran && ! -e $evidence/source/stdout ]] || exit 1 ;;
      esac
      [[ $rc == 75 && ! -e $evidence/source/transport ]] || exit 1
      if [[ $bad == interrupted ]]; then
        [[ $(<"$evidence/source/process_exit") == 255 ]] && cmp "$evidence/source/stdout" "$tmp/operator/source/stdout" || exit 1
      fi
    done
  ) || fail "operator transport did not preserve the original SSH result"
  host="$tmp/host-unstarted"
  printf '1.2.3\n' >"$base_repo/hololive/hololive-api/VERSION"
  mkdir -p "$base_repo/hololive/hololive-alarm-worker"
  cp "$base_repo/hololive/hololive-api/VERSION" "$base_repo/hololive/hololive-alarm-worker/VERSION"
  cp "$base_repo/hololive/hololive-api/VERSION" "$base_repo/VERSION"
  migrator_path=deploy/quadlet/hololive-bot/hololive-db-migrate.container
  git -C "$ROOT_DIR" show "efafcf4005878f53ff239f8e8a1b6255e3b59b75:$migrator_path" >"$base_repo/$migrator_path"
  cp "$ROOT_DIR/scripts/deploy/lib/migration-receipts.sh" "$base_repo/scripts/deploy/lib/"
  commit_repo "$base_repo" legacy-version
  make_host "$host" 0
  run_sync "$host" "$base_repo" || { cat "$host/out" >&2; fail "legacy installation failed"; }
  mkdir -p "$host/opt/current/"{data,logs,backups,runtime-config,artifacts/worker-profile} "$host/run/hololive-bot/postgres" "$host/profile-fixture"
  chmod 0700 "$host/opt/current"
  cp -a "$base_repo" "$host/prior-source"
  prior="$host/prior-source"
  first_sealed=6d7e8749983723867fa52d5a0e9cd648198d2e61
  delta_base=d92cf34bcc24715e60edfc4de5c2fabe5175b7db

  migrator_path=deploy/quadlet/hololive-bot/hololive-db-migrate.container
  api_path=hololive/hololive-api
  git -C "$ROOT_DIR" archive "$first_sealed" CHANGELOG.md VERSION deploy/quadlet/README.md hololive/{hololive-api,hololive-alarm-worker}/VERSION \
    scripts/deploy/{lib/sealed-app.sh,quadlet-redeploy-service.sh,sealed-app-case.sh,sync-opt-current.sh,sync-opt-current_test.sh} \
    "$api_path/internal/migrationrunner/queries" "$api_path/scripts/migrations" | tar -x -C "$prior"
  rm "$prior/$api_path/scripts/migrations/001_legacy.sql"
  cp "$ROOT_DIR/$migrator_path" "$prior/$migrator_path"
  commit_repo "$prior" first-sealed-source
  source_revision="$(git -C "$prior" rev-parse HEAD)"
  writer_revision="$(git -C "$base_repo" rev-parse HEAD)"
  cp -a "$prior" "$host/prior-tool"
  successor="$tmp/successor"
  cp -a "$prior" "$successor"
  mapfile -t inherited < <(git -C "$ROOT_DIR" diff --name-only "$first_sealed" "$delta_base")
  git -C "$ROOT_DIR" archive "$delta_base" "${inherited[@]}" | tar -x -C "$successor"
  commit_repo "$successor" released-parent
  parent_revision="$(git -C "$successor" rev-parse HEAD)"
  for path in "${changed[@]}"; do cp -p "$ROOT_DIR/$path" "$successor/$path"; done
  commit_repo "$successor" successor-source
  git -C "$ROOT_DIR" diff --no-renames --raw "$delta_base" | awk '{print $1,$2,$5,$6}' >"$tmp/expected-delta"
  git -C "$successor" diff --no-renames --raw "$parent_revision" HEAD | awk '{print $1,$2,$5,$6}' >"$tmp/candidate-delta"
  [[ $(wc -l <"$tmp/expected-delta") == 15 ]]
  cmp "$tmp/expected-delta" "$tmp/candidate-delta"
  (
    export GIT_INDEX_FILE="$tmp/review-index"
    rejected() {
      local reason=$1 tree modified
      tree=$(git -C "$successor" write-tree) || exit 1
      modified=$(git -C "$successor" -c user.name=fixture -c user.email=fixture@example.invalid commit-tree "$tree" -p "$parent_revision" -m rejected-delta) || exit 1
      if bash -eu -o pipefail -c '. "$1"; sealed_supersession_delta "$2" "$3"' _ \
          "$ROOT_DIR/scripts/deploy/lib/sealed-app.sh" "$successor" "$modified" 2>"$tmp/delta-error"; then exit 1; fi
      grep -Fxq "$reason" "$tmp/delta-error" || exit 1
      git -C "$successor" read-tree HEAD || exit 1
    }
    git -C "$successor" read-tree HEAD || exit 1
    blob=$(printf 'unauthorized edit\n' | git -C "$successor" hash-object -w --stdin) || exit 1
    git -C "$successor" update-index --cacheinfo "100755,$blob,scripts/deploy/sealed-app-case.sh" || exit 1
    rejected 'changed inherited sealed test'
    git -C "$successor" update-index --chmod=-x scripts/deploy/lib/unstarted-source.py || exit 1
    rejected 'unapproved supersession paths or modes'
    git -C "$successor" update-index --add --cacheinfo "100644,$blob,README.md" || exit 1
    rejected 'unapproved supersession paths or modes'
    git -C "$successor" update-index --force-remove scripts/deploy/unstarted-source-case-evidence.py || exit 1
    rejected 'unapproved supersession paths or modes'
  ) || fail 'unapproved supersession delta accepted'
  supersession_path() {
    bash -eu -c '. "$1"; sealed_supersession_path "$2" "$3" "$4"' _ "$ROOT_DIR/scripts/deploy/lib/sealed-app.sh" "$@"
  }
  path_refused() {
    local reason
    if reason=$(supersession_path "$1" "$2" "$3"); then fail "supersession path accepted: $3"; fi
    [[ $reason == "$4" ]] || fail "unexpected supersession path refusal: $reason"
  }
  path_refused M 100644 hololive/hololive-api/go.mod 'unapproved supersession delta: hololive/hololive-api/go.mod'
  path_refused A 100644 scripts/deploy/lib/unstarted-source.py 'unexpected new supersession executable: scripts/deploy/lib/unstarted-source.py'
  path_refused M 100644 scripts/deploy/supersession-window.py 'unexpected new supersession helper mode: scripts/deploy/supersession-window.py'
  path_refused A 100644 VERSION 'supersession requires an existing reviewed path: VERSION'
  while IFS= read -r -d '' header && IFS= read -r -d '' path; do
    read -r _ new_mode _ _ status <<<"${header#:}"
    supersession_path "$status" "$new_mode" "$path" >/dev/null || fail "reviewed supersession path refused: $path"
  done < <(git -C "$ROOT_DIR" diff --no-renames --no-ext-diff --raw --no-abbrev -z "$first_sealed" HEAD)
  stage_recovery_fixture() {
    local fixture_dir=$1 tag clone
    tag="v$(<"$successor/VERSION")"
    git -C "$successor" -c user.name=fixture -c user.email=fixture@example.invalid tag -a "$tag" -m successor
    git -C "$successor" bundle create "$host/staged.bundle" "refs/tags/$tag"
    cp -a "$successor" "$host/follow-source"
    local follow_tool="$host/follow-tool" path
    git init -q --template= "$follow_tool"
    git -C "$invoking_checkout" ls-files -z scripts/deploy | tar -C "$invoking_checkout" -cf - --null -T - | tar -xf - -C "$follow_tool"
    commit_repo "$follow_tool" invoking-publisher
    for path in scripts/deploy/{quadlet-redeploy-service.sh,quadlet-rollback-service.sh,quadlet-prestart.sh,lib/{app-source-authority.py,sealed-app.sh,protected-binds.sh,worker-profile-pair.py,collector-slots.sh,collector-slots.tsv}}; do
      cp -p "$follow_tool/$path" "$host/follow-source/$path"
    done
    printf '\nfollow-up fixture\n' >>"$host/follow-source/CHANGELOG.md"
    commit_repo "$host/follow-source" follow-on
    successor="$fixture_dir/staged-api"
    export SYNC_TEST_TOOL="$fixture_dir/repo-reviewed-tool"
    for clone in "$successor" "$SYNC_TEST_TOOL" "$host/worker-source"; do
      git init -q --template= "$clone"
      git -C "$clone" fetch -q --no-tags "$host/staged.bundle" "refs/tags/$tag:refs/tags/$tag"
      git -C "$clone" -c advice.detachedHead=false checkout -q --detach "$tag"
    done
    git -C "$successor" rev-parse HEAD >"$host/candidate-revision"
    printf '%s\n' "$source_revision" >"$host/profile-fixture/source-revision"
    python3 -ISB "$ROOT_DIR/scripts/deploy/unstarted-source-case-pairs.py" "$SYNC_TEST_TOOL" "$host" "$writer_revision" legacy
    cp --preserve=all "$host/opt/current/deploy/postgres/pg_hba.conf" "$host/stale-bind"
  }
  stage_recovery_fixture "$tmp"
  run_fault_case() {
    local fault=$1 scenario="$tmp/$1"
    cp -a "$host" "$scenario"
    printf '%s\n' "$fault" >"$scenario/profile-fixture/scenario"
    run_sync "$scenario" "$successor" --unstarted-source || { tail -20 "$scenario/out" >&2; fail "$fault"; }
    tail -1 "$scenario/out"
  }
  run_fault_group() {
    local -a cases=()
    local fault pid status=0
    for fault in "$@"; do
      run_fault_case "$fault" &
      cases+=("$!")
    done
    for pid in "${cases[@]}"; do wait "$pid" || status=1; done
    (( status == 0 )) || fail "$*"
  }
  run_fault_group late-writer pending-worker
  run_fault_group pre-reload-job late-pending
  run_fault_group pre-overlay-job pre-activate-job
  run_fault_group merged-schema merged-writer
  run_fault_group unaccounted-install pending-nop writer-native-dropin dependency-job
  run_fault_group health-at-sync external-reload missing-optional
  run_fault_group lost-response pending-during-peer
  run_fault_group run-before-config merge-extra-run late-restore
  run_sync "$host" "$successor" --unstarted-source || { cat "$host/out" >&2; fail "unstarted source qualification failed"; }
  cat "$host/out"
  qualify_full_history() {
    local fixture_dir="$tmp/full" host writer_revision source_revision installed legacy prior successor tree candidate path SYNC_TEST_TOOL
    mkdir "$fixture_dir"
    cp "$tmp/"{transport-command,bootstrap-command} "$fixture_dir/"
    host="$fixture_dir/host"
    make_host "$host" 0
    writer_revision=efafcf4005878f53ff239f8e8a1b6255e3b59b75
    source_revision=$first_sealed
    installed="$host/usr-local-libexec/hololive-bot"
    mkdir -p "$installed/lib" "$host/profile-fixture" "$host/run/hololive-bot/postgres"
    rm "$host/opt/current/marker" "$host/etc-containers-systemd/hololive-bot/"{stale.container,hololive-alarm-worker.container.d/40-x-spaces.conf}
    git -C "$ROOT_DIR" -c tar.umask=0022 archive "$writer_revision" | tar -xp -C "$host/opt/current"
    legacy="$host/opt/current"
    printf '%s\n' "$writer_revision" >"$legacy/.source-revision"
    chmod 0700 "$legacy"
    mkdir -p "$legacy"/{data,logs,backups,runtime-config,artifacts/worker-profile}
    touch "$host/run-lock/hololive-quadlet-redeploy.lock" "$host/profile-fixture/full-history"
    install -m755 "$legacy/scripts/deploy/quadlet-prestart.sh" "$installed/quadlet-prestart"
    for path in admin-bind public-bind-mounts postgres-capacity compose-env postgres-endpoint; do
      install -m644 "$legacy/scripts/deploy/lib/$path.sh" "$installed/lib/"
    done
    install -m644 "$legacy/scripts/systemd/admin-dashboard-ingress-firewall.service" "$host/etc-systemd-system/"
    install -m644 "$legacy/scripts/systemd/admin-dashboard-ingress.nft" "$host/etc-nftables.d/"
    install -m644 "$legacy"/deploy/quadlet/hololive-bot/* "$host/etc-containers-systemd/hololive-bot/"
    prior="$host/prior-source"
    git init -q --template= "$prior"
    git -C "$prior" fetch -q --no-tags "$ROOT_DIR" HEAD
    git -C "$prior" -c advice.detachedHead=false checkout -q --detach "$first_sealed"
    cp -a "$prior" "$host/prior-tool"
    successor="$fixture_dir/successor"
    cp -a "$prior" "$successor"
    git -C "$successor" checkout -q --detach "$delta_base"
    git -C "$ROOT_DIR" diff --binary "$delta_base" | git -C "$successor" apply --index --binary
    tree=$(git -C "$successor" write-tree)
    candidate=$(git -C "$ROOT_DIR" rev-parse HEAD)
    if [[ $tree != "$(git -C "$ROOT_DIR" rev-parse 'HEAD^{tree}')" ]]; then
      candidate=$(git -C "$successor" -c user.name=fixture -c user.email=fixture@example.invalid commit-tree "$tree" -p "$delta_base" -m reviewed-worktree)
    fi
    git -C "$successor" checkout -q --detach "$candidate"
    stage_recovery_fixture "$fixture_dir"
    run_sync "$host" "$successor" --unstarted-source || { tail -30 "$host/out" >&2; fail "full release trees"; }
    tail -1 "$host/out"
  }
  qualify_full_history
  pass "unstarted legacy predecessor rejection and forward publication"
  exit
fi
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
for lib in admin-bind.sh public-bind-mounts.sh postgres-capacity.sh compose-env.sh postgres-endpoint.sh collector-slots.sh collector-slots.tsv; do
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
    nonroot-owner)
      if (( EUID == 0 )); then chown 1 "$entry"; else touch "$bad_host/foreign-owner-fixture"; fi ;;
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
endpoint_path=scripts/deploy/lib/postgres-endpoint.sh
cp -- "$ROOT_DIR/$endpoint_path" "$generation_repo/$endpoint_path"
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
bad_host="$tmp/host-tampered-sealed-helper"
cp -a "$generation_host" "$bad_host"
touch "$bad_host/tamper-staged-helper"
assert_rejected_without_change "$bad_host" "$generation_repo" "sealed endpoint changed after archive validation" "staged libexec differs from its candidate source" "${generation_args[@]}"
bad_host="$tmp/host-matching-tampered-endpoint"
cp -a "$generation_host" "$bad_host"
printf 'not the accepted helper\n' >>"$bad_host/opt/current/$endpoint_path"
cp "$bad_host/opt/current/$endpoint_path" "$bad_host/usr-local-libexec/hololive-bot/lib/postgres-endpoint.sh"
assert_rejected_without_change "$bad_host" "$generation_repo" "matching unreviewed libexec copies" "installed libexec differs from its protected source" "${generation_args[@]}"
exchanged="$tmp/host-generation-exchange-race"
cp -a "$generation_host" "$exchanged"
printf '%s\n' "$source_revision" >"$exchanged/events.exchange-race"
status=0
run_sync "$exchanged" "$generation_repo" "${generation_args[@]}" || status=$?
[[ $status == 75 && $(<"$exchanged/opt/app-sources/active") == "$candidate_revision" ]] || fail "displaced marker restore was accepted"
displaced=("$exchanged"/opt/.quadlet-check.*/active)
[[ ${#displaced[@]} == 1 && $(<"${displaced[0]}") == "$source_revision" ]] || fail "displaced marker lost"
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
cmp "$source_repo/$endpoint_path" "$generation_host/usr-local-libexec/hololive-bot/lib/postgres-endpoint.sh"
cmp "$generation_repo/$endpoint_path" "$store/$candidate_revision/$endpoint_path"
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
python3 -ISB "$ROOT_DIR/scripts/deploy/unstarted-source-case-pairs.py" "$SYNC_TEST_TOOL" "$generation_host" "$candidate_revision" sealed
bad_host="$tmp/host-wrong-loaded-profile"
cp -a "$generation_host" "$bad_host"
printf '%064d\n' 0 >"$bad_host/profile-fixture/$(printf '%064d.loaded' 8)"
assert_rejected_without_change "$bad_host" "$generation_repo" "unaccepted live profile" "live profile verification failed" \
  --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}"
final_health="$tmp/host-final-writer-health"
cp -a "$generation_host" "$final_health"
touch "$final_health/events.final-writer"
status=0
run_sync "$final_health" "$generation_repo" --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}" || status=$?
[[ $status == 75 && $(<"$final_health/opt/app-sources/active") == "incomplete $candidate_revision -> $next_revision" ]] &&
  grep -Fxq 'injected final writer health change' "$final_health/events" || fail "unhealthy writer published at exchange"
first_race="$tmp/host-first-exchange"
cp -a "$generation_host" "$first_race"
touch "$first_race/events.first-exchange"
old_inode=$(stat -c %i "$first_race/opt/app-sources/active")
status=0
run_sync "$first_race" "$generation_repo" --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}" || status=$?
displaced=("$first_race"/opt/.quadlet-check.*/incomplete)
[[ $status == 75 && $(<"$first_race/opt/app-sources/active") == "incomplete $candidate_revision -> $next_revision" &&
   ${#displaced[@]} == 1 && $(<"${displaced[0]}") == "$candidate_revision" && $(stat -c %i "${displaced[0]}") != "$old_inode" ]] ||
  fail "first exchange accepted a restored predecessor pointer"
interrupted="$tmp/host-generation-between-records"
cp -a "$generation_host" "$interrupted"
touch "$interrupted/events.fail-incomplete"
status=0
run_sync "$interrupted" "$generation_repo" --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}" || status=$?
[[ "$status" == 75 && "$(<"$interrupted/opt/app-sources/active")" == "incomplete $candidate_revision -> $next_revision" &&
  ! -e "$interrupted/opt/app-sources/transition-$candidate_revision-$next_revision" ]] || fail "old authority survived follow-on invalidation"
for signal in TERM HUP INT QUIT ALRM XCPU USR1 USR2 VTALRM PROF IO PWR sync-error full-pipe file-limit; do
  signaled="$tmp/host-generation-$signal"
  cp -a "$generation_host" "$signaled"
  printf '%s\n' "$signal" >"$signaled/events.interrupt"
  status=0
  run_sync "$signaled" "$generation_repo" --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}" || status=$?
  [[ $status == 75 && "$(<"$signaled/opt/app-sources/active")" == "incomplete $candidate_revision -> $next_revision" ]] ||
    { cat "$signaled/out" >&2; fail "$signal with closed stderr lost the unknown outcome (exit=$status active=$(<"$signaled/opt/app-sources/active"))"; }
  git -C "$SYNC_TEST_TOOL" diff --exit-code -- scripts/deploy/sync-opt-current.sh
done
for signal in cleanup-term cleanup-error; do
  signaled="$tmp/host-generation-$signal"
  cp -a "$generation_host" "$signaled"
  printf '%s\n' "$signal" >"$signaled/events.interrupt"
  status=0
  run_sync "$signaled" "$generation_repo" --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}" || status=$?
  expected=0
  [[ $signal != cleanup-term ]] || expected=75
  [[ $status == "$expected" && $(<"$signaled/opt/app-sources/active") == "$next_revision" ]] || fail "$signal after durable publication"
done
saved=("$interrupted"/opt/.quadlet-check.*/transition/previous.actual/.source-revision)
[[ "${#saved[@]}" == 1 && "$(<"${saved[0]}")" == "$candidate_revision" ]] || fail "unpublished transition lost its recovery artifact"
run_sync "$generation_host" "$generation_repo" --protected-source "$candidate_revision" "$next_revision" "$tool_revision" "${generation_args[4]}" ||
  { cat "$generation_host/out" >&2; fail "sealed predecessor was not admitted"; }
[[ "$(<"$store/active")" == "$next_revision" && "$(<"$generation_host/opt/current/.source-revision")" == "$source_revision" ]] ||
  fail "follow-on publication changed the runtime root"
pass "protected generations pin A/M/D archives without replacing runtime or bound inodes and retain verified predecessors"

qualify_released_predecessor() (
  local old=efafcf4005878f53ff239f8e8a1b6255e3b59b75 new=77481e8dfb7924e4bca87e0bfcfedb94e2f1cced
  local fixture="$tmp/released-predecessor" endpoint=scripts/deploy/lib/postgres-endpoint.sh prestart=scripts/deploy/quadlet-prestart.sh
  local old_blob new_blob accepted image_root installed_lib sealed receipt path protected_before legacy_before
  local repo="$fixture/repo" host="$fixture/host" tool="$fixture/tool" installed="$fixture/host/usr-local-libexec/hololive-bot"
  local -a args
  mkdir -p "$fixture"
  git init --quiet --template= "$repo"
  git -C "$repo" fetch -q --no-tags "$ROOT_DIR" "$new"
  git -C "$repo" merge-base --is-ancestor "$old" "$new" || fail "local release history must contain the complete accepted precursor"
  git -C "$repo" -c advice.detachedHead=false checkout -q --detach "$old"
  old_blob="$(git -C "$repo" rev-parse "$old:$endpoint")"
  new_blob="$(git -C "$repo" rev-parse "$new:$endpoint")"
  [[ "$old_blob" != "$new_blob" ]] || fail "released endpoint helper split not exercised"

  make_host "$host" 1
  rm -- "$host/opt/current/marker" "$host/etc-containers-systemd/hololive-bot/stale.container"
  git -C "$repo" -c tar.umask=0022 archive "$old" | tar -xp -C "$host/opt/current"
  printf '%s\n' "$old" >"$host/opt/current/.source-revision"
  mkdir -p "$host/opt/current/"{data,backups,artifacts/worker-profile} "$host/run/hololive-bot/postgres" "$installed/lib"
  touch "$host/run-lock/hololive-quadlet-redeploy.lock"
  install -m755 "$repo/$prestart" "$installed/quadlet-prestart"
  for path in admin-bind public-bind-mounts postgres-capacity compose-env postgres-endpoint; do
    install -m644 "$repo/scripts/deploy/lib/$path.sh" "$installed/lib/$path.sh"
  done
  install -m644 "$repo/scripts/systemd/admin-dashboard-ingress-firewall.service" "$host/etc-systemd-system/"
  install -m644 "$repo/scripts/systemd/admin-dashboard-ingress.nft" "$host/etc-nftables.d/"
  install -m644 "$repo"/deploy/quadlet/hololive-bot/* "$host/etc-containers-systemd/hololive-bot/"
  for path in hololive-api hololive-alarm-worker; do
    install -m644 "$repo/deploy/quadlet/optional/x-spaces/$path.container.d/40-x-spaces.conf" \
      "$host/etc-containers-systemd/hololive-bot/$path.container.d/"
  done
  chmod 0700 "$host/opt/current"
  chmod g+w "$host/opt/current/$migrations" "$host/opt/current/$migrations/"*.sql \
    "$host/opt/current/deploy/postgres/pg_hba.conf" "$host/opt/current/deploy/pgbouncer/"{pg_hba.conf,stats-users.txt}
  installed_lib="$installed/lib/postgres-endpoint.sh"
  [[ "$(stat -c '%a:%h' "$installed_lib")" == 644:1 && "$(stat -c '%a:%h' "$installed/quadlet-prestart")" == 755:1 &&
    "$(git hash-object --no-filters "$installed_lib")" == "$old_blob" &&
    "$(git hash-object --no-filters "$installed/quadlet-prestart")" == "$(git -C "$repo" rev-parse "$old:$prestart")" ]] ||
    fail "fixture did not install the actual predecessor libexec"
  cp --preserve=all "$host/opt/current/deploy/postgres/pg_hba.conf" "$host/stale-bind"
  git -C "$repo" -c advice.detachedHead=false checkout -q --detach "$new"
  git init --quiet --template= "$tool"
  git -C "$tool" fetch -q --no-tags "$repo" HEAD
  git -C "$tool" -c advice.detachedHead=false checkout -q --detach FETCH_HEAD
  cp -- "$ROOT_DIR/scripts/deploy/sync-opt-current.sh" "$tool/scripts/deploy/sync-opt-current.sh"
  commit_repo "$tool" reviewed-predecessor-publisher
  export SYNC_TEST_TOOL="$tool"
  accepted="$(git -C "$tool" rev-parse HEAD)"
  printf '%s\n' "$new" >"$host/candidate-revision"
  args=(--protected-source "$old" "$new" "$accepted" /opt/hololive-bot/release-sources/candidate/source)

  cp --preserve=all "$installed_lib" "$fixture/original-endpoint"
  printf '\n' >>"$installed_lib"
  cp -- "$installed_lib" "$host/opt/current/$endpoint"
  assert_rejected_without_change "$host" "$repo" "tampered installed predecessor helper" "installed libexec differs from its protected source" "${args[@]}"
  cp --preserve=all "$fixture/original-endpoint" "$installed_lib"
  git -C "$repo" show "$old:$endpoint" >"$host/opt/current/$endpoint"
  printf '\n' >>"$repo/$endpoint"
  assert_rejected_without_change "$host" "$repo" "dirty release helper" "clean checkout" "${args[@]}"
  git -C "$repo" show "$new:$endpoint" >"$repo/$endpoint"

  protected_before="$(source_protected "$host")"
  legacy_before="$(stat -c '%d:%i %a %u:%g %y %z' "$host/opt/current" "$host/opt/current/.source-revision" "$host/opt/current/$endpoint")"
  run_sync "$host" "$repo" "${args[@]}" || { cat "$host/out" >&2; fail "full released predecessor transition failed"; }
  image_root="$host/opt/app-sources"
  sealed="$image_root/$new"
  receipt="$image_root/transition-$old-$new"
  [[ "$(<"$image_root/active")" == "$new" && "$(<"$sealed/.source-revision")" == "$new" &&
    "$(<"$host/opt/current/.source-revision")" == "$old" && "$(<"$image_root/$old/.source-revision")" == "$old" &&
    "$(<"$receipt/previous.actual/.source-revision")" == "$old" ]] || fail "released source stamp mismatch"
  [[ "$(source_protected "$host")" == "$protected_before" &&
    "$(stat -c '%d:%i %a %u:%g %y %z' "$host/opt/current" "$host/opt/current/.source-revision" "$host/opt/current/$endpoint")" == "$legacy_before" &&
    "$(git hash-object --no-filters "$installed_lib")" == "$old_blob" &&
    "$(git hash-object --no-filters "$sealed/$endpoint")" == "$new_blob" ]] || fail "sealed helper publication replaced the live predecessor"
  for path in "$prestart" "$endpoint" "$migrations/manifest.txt"; do
    cmp "$receipt/previous.actual/$path" "$host/opt/current/$path"
    cmp "$image_root/$old/$path" "$host/opt/current/$path"
    cmp "$sealed/$path" "$repo/$path"
  done
  cmp "$receipt/changes.raw" <(git -C "$repo" diff --no-renames --no-ext-diff --raw --no-abbrev -z "$old" "$new")
  cmp "$receipt/previous.metadata" "$receipt/previous.after"
  cmp "$receipt/protected.before" "$receipt/protected.after"
  pass "complete released Git trees publish across a real versioned libexec change without touching the installed helper"
)
if [[ "$released" == true ]]; then qualify_released_predecessor; fi

for path in opt/current/deploy/postgres/pg_hba.conf opt/current/deploy/pgbouncer/stats-users.txt \
    opt/current/hololive/hololive-api/scripts/init-db/00-assert-pg19-runtime.sql \
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
