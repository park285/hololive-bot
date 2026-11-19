#!/usr/bin/env bash
set -euo pipefail
[[ $# == 5 && "$EUID" != 0 ]] || exit 2
repo="$(cd "$(dirname "$0")/../.." && pwd -P)"
host="$1"; fakebin="$2"; revision="$3"; api="$4"; worker="$5"
old=efafcf4005878f53ff239f8e8a1b6255e3b59b75
compose=/opt/hololive-bot/compose
legacy="$compose/current"; store="$compose/app-sources"; src="$store/$revision"
units=/etc/containers/systemd/hololive-bot
mkdir -p "$host"/{usr,proc,dev,tmp,stubs,etc/pgbouncer,etc/stack-secrets/hololive-bot/worker-profiles,run/hololive-bot/{postgres,pgbouncer},run/systemd/generator} "$host$src"
mkdir -p "$host/run/lock"
touch "$host/run/lock/hololive-quadlet-redeploy.lock"
trap 'chmod -R u+w -- "$host$store"' EXIT
for d in bin lib lib64 sbin; do ln -s "usr/$d" "$host/$d"; done
cp -R "$fakebin" "$host/fake"
git -C "$repo" -c tar.umask=0022 archive "$old" -- deploy hololive/hololive-api/scripts scripts/deploy scripts/systemd scripts/ci/postgres-capacity-policy.tsv hololive/hololive-api/VERSION | tar -xf - -C "$host$legacy"
printf '%s\n' "$old" >"$host$legacy/.source-revision"
cp "$host$legacy/deploy/quadlet/hololive-bot/"* "$host$units/"
for p in deploy/postgres/pg_hba.conf deploy/pgbouncer/{pg_hba.conf,stats-users.txt}; do
  chmod g+w "$host$legacy/$p"
  mkdir -p "$host/bind-files/${p%/*}"
  cp --preserve=all "$host$legacy/$p" "$host/bind-files/$p"
done
chmod g+w "$host$legacy/scripts/deploy/quadlet-redeploy-service.sh" "$host$legacy/scripts/deploy/quadlet-rollback-service.sh"
(cd "$repo" && tar -cf - scripts/deploy scripts/ci/postgres-capacity-policy.tsv scripts/systemd deploy/{quadlet,postgres,pgbouncer} hololive/hololive-api/{scripts/init-db,scripts/migrations,internal/migrationrunner/queries,VERSION} hololive/hololive-alarm-worker/VERSION) | tar -xf - -C "$host$src"
printf '%s\n' "$revision" | tee "$host$store/active" >"$host$src/.source-revision"
mkdir -m700 "$host$store/$old" "$host$store/transition-$old-$revision"
git -C "$repo" -c tar.umask=0222 archive "$old" -- scripts/deploy/quadlet-prestart.sh scripts/deploy/lib | tar -xf - -C "$host$store/$old"
printf '%s\n' "$old" >"$host$store/$old/.source-revision"; chmod 0400 "$host$store/$old/.source-revision"; chmod 0500 "$host$store/$old"
printf 'installed=%s\ncandidate=%s\ntool=%s\n' "$old" "$revision" "$revision" >"$host$store/transition-$old-$revision/revisions"
chmod 0600 "$host$store/transition-$old-$revision/revisions"
find "$host$src" -type d -exec chmod 0555 {} +
find "$host$src" -type f -perm /111 -exec chmod 0555 {} +
find "$host$src" -type f ! -perm /111 -exec chmod 0444 {} +
chmod 0700 "$host$legacy" "$host$store"; chmod 0500 "$host$src"; chmod 0400 "$host$src/.source-revision"; chmod 0600 "$host$store/active"
rm "$host$units/"{hololive-api,hololive-alarm-worker}.container.d/50-worker-profile.conf
cp "$host/state/live-pgbouncer.ini" "$host/run/hololive-bot/pgbouncer/pgbouncer.ini"
mkdir -p "$host/libexec/hololive-bot/lib" "$host/etc/nftables.d"
install -m755 "$host$legacy/scripts/deploy/quadlet-prestart.sh" "$host/libexec/hololive-bot/quadlet-prestart"
cp "$host$legacy/scripts/deploy/lib/"{admin-bind,public-bind-mounts,postgres-capacity,compose-env,postgres-endpoint}.sh "$host/libexec/hololive-bot/lib/"
cp "$host$legacy/scripts/systemd/admin-dashboard-ingress.nft" "$host/etc/nftables.d/"
cat >"$host/stubs/ip" <<'SH'
#!/usr/bin/env bash
[[ "$*" == '-4 -o addr show dev tailscale0' ]] || exit 2
echo '5: tailscale0 inet 100.100.1.8/32 scope global tailscale0'
SH
cat >"$host/stubs/tailscale" <<'SH'
#!/usr/bin/env bash
[[ "$*" == 'ip --assert 100.100.1.8' ]]
SH
cat >"$host/stubs/nft" <<'SH'
#!/usr/bin/env bash
[[ "$*" == '-j list table inet admin_dashboard_ingress' ]] || exit 2
cat "$SRC/scripts/deploy/testdata/ingress-firewall.json"
SH
for n in hololive-api alarm-worker; do printf '{"fixture":"%s"}\n' "$n" >"$host/etc/stack-secrets/hololive-bot/worker-profiles/$n.json"; done
for n in bot alarm-worker; do printf 'FIXTURE=1\n' >"$host/etc/stack-secrets/hololive-bot/$n.env"; done
for n in hololive-api hololive-alarm-worker; do
  i=8; p=6; if [[ "$n" == hololive-alarm-worker ]]; then i=9; p=5; fi
  printf '%064d\n' "$i" >"$host/state/$n/image"
  for number in "$i" "$p"; do
    id="$(printf '%064d' "$number")"
    printf 'id=%s\nplatform=linux/arm64\ntitle=%s\nrevision=%s\nschema=text-file-v1\n' "$id" "$n" "$old" >"$host/state/images/sha256_$id"
  done
done
cat >"$host/stubs/podman" <<'SH'
#!/usr/bin/env bash
set -eu
case "$1 $2" in
 'events --stream=false') cat /state/migrator-exit; exit ;;
 'container inspect') if [[ "$4" == '{{.State.Pid}} {{.State.Health.Status}}' ]]; then echo "$MOUNT_PID healthy"; exit; fi ;;
 'inspect -f') if [[ "$3" == '{{.State.Running}} {{.Id}} {{.Image}} {{.State.ConmonPid}}' ]]; then
   dir="/state/$4"; printf '%s %s %s %s\n' "$(cat "$dir/active")" "$(cat "$dir/cid")" "$(cat "$dir/image")" "$(cat "$dir/conmonpid")"; exit; fi ;;
 'image inspect')
   if [[ "$3" == sha256:* ]]; then
     revision="$(sed -n 's/^revision=//p' "/state/images/sha256_${3#sha256:}")"
     printf '[{"Id":"%s","Config":{"Labels":{"org.opencontainers.image.revision":"%s"}},"Architecture":"arm64","Os":"linux"}]\n' "${3#sha256:}" "$revision"; exit
   elif [[ "$4" == *image.version* ]]; then
     n=hololive-api; [[ "$5" != "$WORKER" ]] || n=hololive-alarm-worker; cat "$SRC/hololive/$n/VERSION"; exit
   fi ;;
 'exec -i') if [[ "$4" == env ]]; then
   sql="$(cat)"; head="$(cat "$SRC/hololive/hololive-api/internal/migrationrunner/queries/lineage_head.sql")"
   [[ "$sql" == *"${head%;}"* && "$sql" == *'pending = 0'* ]] || exit 2
   printf 't\n'; exit
 fi ;;
esac
if [[ "$1" == exec && "$2" =~ ^[0-9a-f]{64}$ && "$3" == ./bin/healthcheck ]]; then printf 'H3 %s\n' "$2" >>/state/calls; exit; fi
exec /fake/podman "$@"
SH
cat >"$host/stubs/journalctl" <<'SH'
#!/usr/bin/env bash
if [[ "$*" == *' _PID=1 '* ]]; then
  read -r job unit invocation at </state/manager-completion
  [[ "$*" == *" UNIT=$unit JOB_ID=$job JOB_RESULT=done "* && ! "$at" < "${6#--since=@}" ]] || exit 1
  printf '{"INVOCATION_ID":"%s"}\n' "$invocation"; exit
fi
[[ "${*: -1}" == CONTAINER_NAME=hololive-db-migrate && -f /state/migrator-exit && "$*" == *" _SYSTEMD_INVOCATION_ID=$(printf '%032d' 97) "* ]] || exit 2
printf '{"CONTAINER_ID_FULL":"%064d","IMAGE_NAME":"%s","MESSAGE":"==> hololive migrations applied (applied=0 skipped=32 total=32)"}\n' 97 "$API"
SH
cat >"$host/stubs/systemctl" <<'SH'
#!/usr/bin/env bash
set -eu
prestart() {
  local -a argv
  read -r -a argv <<<"$(sed -n 's/^ExecStartPre=//p' "/run/systemd/generator/$1.service" | tail -1)"
  "${argv[@]}"
}
job=
if [[ "$1" == --show-transaction ]]; then
  shift
  [[ "$1" == --job-mode=ignore-dependencies ]] || exit 2
  job=$((1000 + $(wc -l </state/calls))); type="$2"
  [[ "$type" != try-restart ]] || type=restart
  printf 'Enqueued anchor job %s %s/%s.\n' "$job" "$3" "$type" >&2
fi
case "$*" in
 daemon-reload) fresh="$(mktemp -d)"; QUADLET_UNIT_DIRS="$FAKE_QUADLET_DIR" /usr/libexec/podman/quadlet "$fresh"; cp -a "$fresh"/. /run/systemd/generator; rm -rf "$fresh"; exit ;;
 'show --all hololive-db-migrate.service -p LoadState -p ActiveState -p SubState -p Job') printf 'LoadState=loaded\nActiveState=inactive\nSubState=dead\nJob=\n'; exit ;;
 'show '*.service' --property=FragmentPath --value') echo "/run/systemd/generator/$2"; exit ;;
 'show '*.service' -p Requires -p Wants -p Requisite -p BindsTo --value') echo holo-pgbouncer.service; exit ;;
 'show hololive-api.service -p Job --value')
   [[ ! -e /state/job-query-failed ]] || exit 3
   if [[ -e /state/job-pending ]]; then echo 84; fi
   exit 0 ;;
 'is-active --quiet holo-pgbouncer.service'|'is-active --quiet admin-dashboard-ingress-firewall.service') exit 0 ;;
 'show admin-dashboard-ingress-firewall.service -p InvocationID --value') printf '%032d\n' 25; exit ;;
 '--job-mode=ignore-dependencies start hololive-db-migrate.service')
   grep -q '^ExecStart=.* --check$' /run/systemd/generator/hololive-db-migrate.service
   prestart hololive-db-migrate
   printf '%064d|hololive-db-migrate|%s|0\n' 97 "$(date +%s%N)" >/state/migrator-exit
   printf '%s %s %032d %s\n' "${job:?}" "$3" 97 "$EPOCHREALTIME" >/state/manager-completion
   printf 'checked %s\n' "$API" >> /state/calls; exit ;;
 '--job-mode=ignore-dependencies try-restart '*.service|'--job-mode=ignore-dependencies restart '*.service)
   n="${3%.service}"; pair="$(cat "$FAKE_DEPLOY_ROOT/artifacts/worker-profile/active/$n.pair")"
   prestart "$n"
   image="$(sed -n 's/^Image=sha256://p' "$pair/50-worker-profile.conf")"
   if [[ "$2" == restart ]]; then next=17; else next="$([[ "$n" == hololive-api ]] && echo 15 || echo 16)"; fi
   printf '%s\n' "$image" >"/state/$n/image"; printf '%064d\n' "$next" >"/state/$n/cid"
   printf '%s %s %032d %s\n' "${job:?}" "$3" "$next" "$EPOCHREALTIME" >/state/manager-completion
   printf '%032d\n' "$next" >"/state/$n/invocation"; echo 0 >"/state/$n/nrestarts"
   printf 'replaced %s\n' "$n" >> /state/calls; exit ;;
 'start '*|'restart '*|'stop '*) echo "unexpected dependency transaction: $1" >&2; exit 2 ;;
esac
exec /fake/systemctl "$@"
SH
cat >"$host/stubs/python3" <<'SH'
#!/usr/bin/env bash
set -eu
if [[ "$1" == "$FAKE_PAIR_TOOL" ]]; then
 case "$2" in
  activate)
   if [[ -f /state/interrupt-pointer && "$4" == */candidate ]]; then
    /usr/bin/python3 -IB - "$@" <<'PY'
import os, runpy, sys
real = os.replace
def interrupt(src, dst):
    if str(dst).endswith('.pair'):
        raise OSError('fixture pointer publication interrupted')
    real(src, dst)
os.replace = interrupt
sys.argv = sys.argv[1:]
runpy.run_path(sys.argv[0], run_name='__main__')
PY
    exit
   fi ;;
  prepare|capture)
   /usr/bin/python3 -IB - "$@" <<'PY'
from pathlib import Path
import os, runpy, sys
file, action, service, directory, name, root = sys.argv[1:]
tool = runpy.run_path(file); d = Path(directory); d.mkdir()
active = Path((Path(root)/'artifacts/worker-profile/active'/(service+'.pair')).read_text().strip())
(d/'profile.json').write_bytes((active/'profile.json').read_bytes())
(d/'unit.container').write_bytes((Path(os.environ['FAKE_QUADLET_DIR'])/(service+'.container')).read_bytes())
identity = tool['image_identity']('sha256:'+(Path('/state')/service/'image').read_text().strip()); identity['container_id']=(Path('/state')/service/'cid').read_text().strip()
if service == 'hololive-api': identity['app_version']='19.2.1'
pair=tool['save_pair'](d,service,identity)
if action=='prepare':
    tool['dropin'](d,service,pair); tool['activate'](d,service,root)
PY
   exit ;;
  check)
    tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
    if [[ "$3" == hololive-api && "$4" == */previous ]]; then args=(--app-version 19.2.1); else args=(); fi
    bash "$SRC/scripts/deploy/quadlet-prestart.sh" --render-env "$3" "$tmp" "${args[@]}"
    if [[ "$3" == hololive-api && "$4" == */candidate ]]; then grep -Fxq "APP_VERSION=$(cat "$SRC/hololive/hololive-api/VERSION")" "$tmp/$3.env"; fi
    exit ;;
  verify-captured) [[ "$(/usr/bin/python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["container_id"])' "$4/pair.json")" == "$(cat "/state/$3/cid")" ]]; exit ;;
  verify-live) grep -Fxq "Image=sha256:$(cat "/state/$3/image")" "$4/50-worker-profile.conf"; exit ;;
 esac
fi
exec /usr/bin/python3 "$@"
SH
chmod 755 "$host/stubs/"*
cat >"$host/drive" <<'SH'
set -euo pipefail
export MOUNT_PID=$$ PATH=/stubs:/fake:/usr/bin:/usr/sbin HOME=/tmp HOLOLIVE_QUADLET_TEST_ROOT= FAKE_STATE=/state FAKE_CASE=success FAKE_COORDINATED=true
export FAKE_REAL_PYTHON3=/usr/bin/python3 FAKE_PAIR_TOOL="$SRC/scripts/deploy/lib/worker-profile-pair.py" FAKE_STAGE_PAIR_TOOL=/absent FAKE_FENCE_DIR=/var/lib/hololive-postgres-fence
export LIVE_LOGS_PATH=/logs VALKEY_SENTINEL_ADDRS=local HOLOLIVE_API_PORT_BIND_IP=127.0.0.1 HOLOLIVE_ADMIN_API_PORT_BIND_IP=100.100.1.8 HOLOLIVE_H3_SERVER_NAME=localhost HOLOLIVE_DB_PASSWORD=fixture HOLOLIVE_ALARM_WORKER_PASSWORD=fixture HOLOLIVE_MIGRATOR_PASSWORD=fixture
helper="$SRC/scripts/deploy/quadlet-redeploy-service.sh"
old_units="$(sha256sum "$FAKE_QUADLET_DIR/"*.container "$FAKE_DEPLOY_ROOT/.source-revision")"
shared_before="$(stat -c '%D:%i %a %Z' /usr/local/libexec/hololive-bot/quadlet-prestart)"
export FAKE_CANDIDATE_ID="$API" FAKE_API_ID="$API" FAKE_WORKER_ID="$WORKER"
systemctl daemon-reload
record="${SRC%/*}/active"
authority=(python3 -ISB "$SRC/scripts/deploy/lib/app-source-authority.py" "$REV")
mv "${SRC%/*}" /opt/hololive-bot/unpublished
absence=0
python3 -ISB "/opt/hololive-bot/unpublished/$REV/scripts/deploy/lib/app-source-authority.py" "$REV" || absence=$?
[[ "$absence" == 3 ]]
mv /opt/hololive-bot/unpublished "${SRC%/*}"
printf '%s\n\n' "$REV" >"$record"
if "${authority[@]}"; then exit 1; else [[ "$?" == 1 ]]; fi
if "$helper" hololive-api "$API"; then exit 1; fi
printf '%s\n' "$REV" >"$record"
chmod u+w "$SRC/scripts/deploy/lib"
ln -s /bin/true "$SRC/scripts/deploy/lib/foreign"
if "$helper" hololive-api "$API"; then exit 1; fi
rm "$SRC/scripts/deploy/lib/foreign"; chmod 0555 "$SRC/scripts/deploy/lib"
mkdir -p "$FAKE_QUADLET_DIR/hololive-db-migrate.container.d"
printf '[Container]\nExec=--initialize\n' >"$FAKE_QUADLET_DIR/hololive-db-migrate.container.d/90-other.conf"
if "$helper" hololive-api "$API"; then exit 1; fi
rm "$FAKE_QUADLET_DIR/hololive-db-migrate.container.d/90-other.conf"
[[ ! -e "$FAKE_DEPLOY_ROOT/artifacts" ]]
peer=/state/hololive-alarm-worker
printf 'false\n' >"$peer/active"
if "$helper" hololive-api "$API"; then exit 1; fi
[[ ! -e "$FAKE_DEPLOY_ROOT/artifacts" ]]
printf 'true\n' >"$peer/active"
rollback="$SRC/scripts/deploy/quadlet-rollback-service.sh"
releases="$FAKE_DEPLOY_ROOT/artifacts/worker-profile/releases"
/usr/bin/python3 -IB - <<'PY'
import os, runpy, subprocess
from pathlib import Path
root = Path(os.environ['FAKE_DEPLOY_ROOT'])
tool = runpy.run_path(str(root / 'scripts/deploy/lib/worker-profile-pair.py'))
for service, old, prior in [('hololive-api', 8, 6), ('hololive-alarm-worker', 9, 5)]:
    r = root / 'artifacts/worker-profile/releases' / (service+'-v2-legacy')
    r.mkdir(parents=True)
    unit = Path(os.environ['FAKE_QUADLET_DIR'],service+'.container')
    for slot, number in [('previous', prior), ('candidate', old)]:
        d = r/slot; d.mkdir()
        (d/'profile.json').write_bytes((Path(tool['SOURCE'])/tool['PROFILES'][service]).read_bytes())
        (d/'unit.container').write_bytes(unit.read_bytes())
        pair = tool['image_identity']('sha256:'+format(number,'064d'))
        if slot == 'previous' and service == 'hololive-api': pair['app_version']='19.2.1'
        tool['save_pair'](d,service,pair)
        if slot == 'candidate':
            tool['dropin'](d,service,pair); tool['activate'](d,service,root)
        else: tool['dropin'](d,service,pair,r/'rollback.conf',pair.get('app_version'))
    for k,v in [('service',service),('candidate.revision',os.environ['OLD']),('candidate.image','sha256:'+format(old,'064d')),('previous.endpoint','holo-pgbouncer|6432')]:
        (r/k).write_text(v+'\n')
    (r/'rollback.sha256').write_bytes(subprocess.check_output(['sha256sum','previous/pair.json','previous.endpoint','rollback.conf'],cwd=r))
PY
systemctl daemon-reload
worker_overlay="$FAKE_QUADLET_DIR/hololive-alarm-worker.container.d/50-worker-profile.conf"
worker_before="$(stat -c '%d:%i:%Z' "$worker_overlay") $(sha256sum "$worker_overlay")"
"$rollback" "$releases/hololive-api-v2-legacy" --check
"$rollback" "$releases/hololive-alarm-worker-v2-legacy" --check
if "$helper" hololive-alarm-worker "$WORKER" "$API"; then exit 1; fi
! grep -q '^checked ' /state/calls
touch /state/interrupt-pointer
if "$helper" hololive-api "$API"; then exit 1; fi
rm /state/interrupt-pointer
partial="$(find "$releases" -name activation.intent -printf '%h\n')"
[[ -f "$partial/before.conf" && "$(cat /state/hololive-api/image)" == "$(printf '%064d' 8)" ]]
"$rollback" "$partial" --check
for uncertain in writer-job job-pending job-query-failed; do
  if [[ "$uncertain" == writer-job ]]; then
    mkdir "$partial/writer-job"; printf 'try-restart hololive-api.service %s %s\n' "$API" "$EPOCHREALTIME" >"$partial/writer-job/request"
  else
    touch "/state/$uncertain"
  fi
  "$rollback" "$partial" --check > /state/check-only
  grep -Fq '[CHECK_ONLY]' /state/check-only
  if "$rollback" "$partial" --apply; then exit 1; fi
  [[ ! -e "$partial/recovery-attempt" && "$(cat /state/hololive-api/cid)" == "$(printf '%064d' 7)" ]]
  if [[ "$uncertain" == writer-job ]]; then rm "$partial/writer-job/request"; rmdir "$partial/writer-job"; else rm "/state/$uncertain"; fi
done
"$rollback" "$partial" --apply
[[ "$(cat /state/hololive-api/cid)" == "$(printf '%064d' 7)" ]]
"$helper" hololive-api "$API"
grep -Fxq "ExecStartPre=/usr/bin/bash $SRC/scripts/deploy/quadlet-prestart.sh hololive-api" "$FAKE_QUADLET_DIR/hololive-api.container.d/50-worker-profile.conf"
[[ "$worker_before" == "$(stat -c '%d:%i:%Z' "$worker_overlay") $(sha256sum "$worker_overlay")" ]]
"$rollback" "$releases/hololive-alarm-worker-v2-legacy" --check
api_pair="$(cat "$FAKE_DEPLOY_ROOT/artifacts/worker-profile/active/hololive-api.pair")"
"$rollback" "${api_pair%/candidate}" --check
if "$rollback" "$releases/hololive-api-v2-legacy" --check; then exit 1; fi
export FAKE_CANDIDATE_ID="$WORKER"
"$helper" hololive-alarm-worker "$WORKER" "$API"
worker_pair="$(cat "$FAKE_DEPLOY_ROOT/artifacts/worker-profile/active/hololive-alarm-worker.pair")"
"$rollback" "${worker_pair%/candidate}" --check
[[ "$(grep -E '^(checked|replaced|H3) ' /state/calls)" == "$(printf 'checked %s\nchecked %s\nreplaced hololive-api\nH3 %064d\nchecked %s\nreplaced hololive-alarm-worker\nH3 %064d\n' "$API" "$API" 15 "$API" 16)" ]]
grep -Fxq "APP_VERSION=$(cat "$SRC/hololive/hololive-api/VERSION")" /run/hololive-bot/quadlet-env/hololive-api.env
"$rollback" "${worker_pair%/candidate}" --apply
[[ "$(cat /state/hololive-alarm-worker/image)" == "$(printf '%064d' 9)" && "$(cat /state/hololive-api/image)" == "${API#sha256:}" ]]
[[ "$shared_before" == "$(stat -c '%D:%i %a %Z' /usr/local/libexec/hololive-bot/quadlet-prestart)" ]]
[[ "$old_units" == "$(sha256sum "$FAKE_QUADLET_DIR/"*.container "$FAKE_DEPLOY_ROOT/.source-revision")" ]]
for dest in /run/hololive-bot/postgres/pg_hba.conf /etc/pgbouncer/{pg_hba.conf,stats-users.txt}; do
  dir="${dest%/*}"; file="$FAKE_DEPLOY_ROOT/deploy/${dir##*/}/${dest##*/}"
  [[ ! "$file" -ef "$dest" && "$(stat -c %a "$file")" == 664 ]]
  cmp -s "$file" "$dest"
done
SH
export SRC="$src" REV="$revision" OLD="$old" API="$api" WORKER="$worker" FAKE_QUADLET_DIR="$units" FAKE_DEPLOY_ROOT="$legacy"
unshare --user --map-root-user --mount bash -eu -c '
  h="$1"
  for d in proc dev; do mount --rbind "/$d" "$h/$d"; done
  for d in bin sbin lib lib64 share libexec; do mkdir -p "$h/usr/$d"; mount --rbind "/usr/$d" "$h/usr/$d"; done
  mkdir -p "$h/usr/local/libexec"
  mount --bind "$h/libexec" "$h/usr/local/libexec"
  for pair in "hololive/hololive-api/scripts/init-db:/docker-entrypoint-initdb.d" "deploy/postgres/pg_hba.conf:/run/hololive-bot/postgres/pg_hba.conf" "deploy/pgbouncer/pg_hba.conf:/etc/pgbouncer/pg_hba.conf" "deploy/pgbouncer/stats-users.txt:/etc/pgbouncer/stats-users.txt" "/run/hololive-bot/pgbouncer/pgbouncer.ini:/etc/pgbouncer/pgbouncer.ini"; do
    s="${pair%:*}"; d="$h${pair#*:}"
    if [[ -f "$h/bind-files/$s" ]]; then s="/bind-files/$s"; elif [[ "$s" != /* ]]; then s="$FAKE_DEPLOY_ROOT/$s"; fi
    if [[ -d "$h$s" ]]; then mkdir -p "$d"; else touch "$d"; fi
    mount --bind "$h$s" "$d"; mount -o remount,bind,ro "$d"
  done
  exec chroot "$h" /usr/bin/bash /drive
' _ "$host"
