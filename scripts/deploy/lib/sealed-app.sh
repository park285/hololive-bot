#!/usr/bin/env bash

sealed_migrator_dropin() {
  local root="$1" image="$2"
  [[ "${root##*/}" =~ ^[0-9a-f]{40}$ && "$image" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  printf '%s\n' '[Container]' "Image=$image" 'Exec=--check' 'Volume=' \
    'Volume=holo-pg-admin-socket.volume:/var/run/postgresql:ro' 'Environment=' \
    'Environment=PGHOST=/var/run/postgresql' 'Environment=PGPORT=5432' 'Environment=PGDATABASE=hololive' \
    'Environment=PGUSER=${HOLOLIVE_MIGRATOR_USER}' 'Environment=PGSSLMODE=disable' \
    'Environment=PGAPPNAME=hololive-db-migrate' '[Service]' 'ExecStartPre=' \
    "ExecStartPre=/usr/bin/bash $root/scripts/deploy/quadlet-prestart.sh hololive-db-migrate schema"
}

sealed_authority() {
  : "${validator:?}" "${prefix?}"
  python3 -ISB - "$validator" "$prefix" "$@" <<'PY'
import os
from pathlib import Path
import runpy
import sys

helper, prefix, kind, *names = sys.argv[1:]
checked = runpy.run_path(helper)['checked']
boundary = Path(prefix or '/')
seen = set()
for name in names:
    path = Path(name)
    if not path.is_relative_to(boundary):
        sys.exit('authority outside installation')
    for directory in reversed(path.parents):
        if directory.is_relative_to(boundary) and directory not in seen:
            checked(directory, True)
            seen.add(directory)
    checked(path, kind in ('tree', 'directory'))
    if kind == 'lock' and not os.path.samestat(path.lstat(), os.fstat(9)):
        sys.exit('unrecognized deployment lock inode')
    if kind == 'tree':
        for base, children, files in os.walk(path, onerror=lambda error: sys.exit(str(error))):
            for child in children:
                checked(Path(base, child), True)
            for child in files:
                checked(Path(base, child), False)
PY
}

sealed_bind_generation() {
  : "${deploy_root:?}" "${ROOT_DIR:?}" "${prefix?}"
  local name="$1" state pid health bind file expected
  local -a binds
  state="$(podman container inspect --format '{{.State.Pid}} {{.State.Health.Status}}' "$name")" || return 1
  read -r pid health <<<"$state"
  [[ "$pid" =~ ^[1-9][0-9]*$ && "$health" == healthy ]] || return 1
  case "$name" in
    holo-postgres) binds=("$deploy_root/hololive/hololive-api/scripts/init-db|/docker-entrypoint-initdb.d"
      "$deploy_root/deploy/postgres/pg_hba.conf|/run/hololive-bot/postgres/pg_hba.conf") ;;
    holo-pgbouncer) binds=("$prefix/run/hololive-bot/pgbouncer/pgbouncer.ini|/etc/pgbouncer/pgbouncer.ini"
      "$deploy_root/deploy/pgbouncer/pg_hba.conf|/etc/pgbouncer/pg_hba.conf"
      "$deploy_root/deploy/pgbouncer/stats-users.txt|/etc/pgbouncer/stats-users.txt") ;;
    *) return 1 ;;
  esac
  for bind in "${binds[@]}"; do
    file="${bind%|*}"
    [[ "$file" == "$deploy_root/"* ]] || continue
    expected="$ROOT_DIR/${file#"$deploy_root/"}"
    if [[ -d "$expected" ]]; then
      diff -qr --no-dereference "$expected" "$file" >/dev/null || return 1
      find "$file" -type f -printf '%P %D:%i %m %U:%G %C@ %T@\n' | LC_ALL=C sort || return 1
    else
      cmp -s "$expected" "$file" || return 1
    fi
  done
  . "$ROOT_DIR/scripts/deploy/lib/protected-binds.sh"
  verify_running_binds "$pid" "${binds[@]}"
}

sealed_generation() {
  local name="$1" id data running cid image pid endpoint
  id="$(systemctl show "$name.service" -p InvocationID --value)" && [[ "$id" =~ ^[0-9a-f]{32}$ && "$id" != 00000000000000000000000000000000 ]] &&
    systemctl is-active --quiet "$name.service" &&
    data="$(podman inspect -f '{{.State.Running}} {{.Id}} {{.Image}} {{.State.ConmonPid}}' "$name")" &&
    read -r running cid image pid <<<"$data" && [[ "$running" == true && "$cid" =~ ^[0-9a-f]{64}$ && "$image" =~ ^[0-9a-f]{64}$ &&
    "$pid" =~ ^[1-9][0-9]*$ && "$(systemctl show "$name.service" -p MainPID --value)" == "$pid" ]] &&
    endpoint="$(postgres_endpoint_container "$cid")" && [[ "$endpoint" == 'holo-pgbouncer|6432' ]] &&
    [[ "$(systemctl show "$name.service" -p InvocationID --value)" == "$id" ]] && printf '%s %s %s %s %s\n' "$name" "$id" "$cid" "$image" "$endpoint"
}

sealed_begin_slot() {
  case "$1" in
    hololive-api) unchanged_peer=hololive-alarm-worker ;;
    hololive-alarm-worker) unchanged_peer=hololive-api ;;
    *) return 1 ;;
  esac
  peer_generation="$(sealed_generation "$unchanged_peer")" || die "unchanged peer is not a running pooled writer: $unchanged_peer"
  original_target_generation=
  if systemctl is-active --quiet "$1.service"; then
    original_target_generation="$(sealed_generation "$1")" || die 'target writer generation unconfirmed'
  fi
  firewall_invocation="$(systemctl show admin-dashboard-ingress-firewall.service -p InvocationID --value)"
  [[ "$firewall_invocation" =~ ^[0-9a-f]{32}$ && "$firewall_invocation" != 00000000000000000000000000000000 ]] &&
    systemctl is-active --quiet admin-dashboard-ingress-firewall.service || die 'unconfirmed running ingress firewall'
}

sealed_assert_peer() {
  [[ "$(sealed_generation "$unchanged_peer")" == "$peer_generation" ]] || die "non-target writer changed: $unchanged_peer"
  systemctl is-active --quiet admin-dashboard-ingress-firewall.service &&
    [[ "$(systemctl show admin-dashboard-ingress-firewall.service -p InvocationID --value)" == "$firewall_invocation" ]] ||
    die 'ingress firewall generation changed'
}

sealed_admit_pair() {
  : "${deploy_root:?}" "${releases:?}" "${revision:?}" "${quadlet_dir:?}" "${pair_tool:?}"
  local slot="$1" live="$2" expected="$3" allow_accepted="$4"
  local pointer="$deploy_root/artifacts/worker-profile/active/$slot.pair" pair dir image sha
  sealed_authority files "$pointer" || return 1
  pair="$(<"$pointer")"; dir="${pair%/candidate}"
  [[ "$pair" == "$dir/candidate" && "${dir%/*}" == "$releases" && "${dir##*/}" == "$slot-v2-"* && "$(realpath -e -- "$pair")" == "$pair" ]] &&
    sealed_authority tree "$dir" || return 1
  image="$(<"$dir/candidate.image")"; sha="$(<"$dir/candidate.revision")"
  [[ "$image" =~ ^sha256:[0-9a-f]{64}$ && "$live" == "$slot "* && "$live" == *" ${image#sha256:} holo-pgbouncer|6432" ]] &&
    cmp -s "$dir/candidate.image" <(printf '%s\n' "$image") && cmp -s "$dir/candidate.revision" <(printf '%s\n' "$sha") &&
    [[ "$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")" == "$sha" &&
       "$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.title"}}' "$image")" == "$slot" ]] || return 1
  if [[ "$sha" == "$revision" ]]; then
    [[ "$allow_accepted" == yes ]] && cmp -s "$dir/accepted-writer" <(printf '%s\n' "$live") || return 1
    [[ "$slot" != hololive-api || "$image" == "${migrator_id:?}" ]] || return 1
  else
    [[ -n "$expected" && "$sha" == "$expected" ]] || return 1
  fi
  cmp -s "$pointer" <(printf '%s\n' "$pair") &&
    cmp -s "$pair/50-worker-profile.conf" "$quadlet_dir/$slot.container.d/50-worker-profile.conf" &&
    python3 "$pair_tool" verify "$slot" "$pair" "$image" && python3 "$pair_tool" verify-live "$slot" "$pair" "$slot"
}

sealed_recovery_images() {
  local dir="$1" candidate="$2" api worker
  local -a images
  sealed_authority files "$dir/images" "$dir/images.after" &&
    cmp -s "$dir/images" "$dir/images.after" && mapfile -t images <"$dir/images" || return 1
  [[ ${#images[@]} == 2 && ${images[0]} =~ ^hololive-api\ (sha256:[0-9a-f]{64})$ ]] || return 1
  api="${BASH_REMATCH[1]}"
  [[ ${images[1]} =~ ^hololive-alarm-worker\ (sha256:[0-9a-f]{64})$ ]] || return 1
  worker="${BASH_REMATCH[1]}"
  [[ "$api" != "$worker" && "$api" == "$migrator_id" ]] &&
    cmp -s "$dir/images" <(printf '%s\n' "${images[@]}") || return 1
  if [[ "$service" == hololive-api ]]; then
    [[ "$candidate" == "$api" && "$(resolve_image "$worker" hololive-alarm-worker)" == "$worker" ]]
  else
    [[ "$candidate" == "$worker" && "$peer_generation" == *" ${api#sha256:} holo-pgbouncer|6432" ]]
  fi
}

sealed_recovery_pair() {
  local slot="$1" live="$2" predecessor="$3" installed="$quadlet_dir/$1.container.d/50-worker-profile.conf"
  local pointer="$deploy_root/artifacts/worker-profile/active/$slot.pair" pair rows offset=0
  sealed_authority files "$installed" "$quadlet_dir/$slot.container" || return 1
  if [[ "$predecessor" == yes ]]; then
    sealed_admit_pair "$slot" "$live" "$recovery_writer_revision" no || return 1
    pair="$(<"$pointer")"
    [[ "$slot" == hololive-api ]] || offset=3
    local -a snapshot
    sealed_authority files "$recovery_receipt/pairs.before" "$recovery_receipt/pairs.after" &&
      cmp -s "$recovery_receipt/pairs.before" "$recovery_receipt/pairs.after" &&
      mapfile -t snapshot <"$recovery_receipt/pairs.after" && [[ ${#snapshot[@]} == 6 ]] &&
      cmp -s "$recovery_receipt/pairs.after" <(printf '%s\n' "${snapshot[@]}") || return 1
    rows="$(sha256sum -- "$pair/pair.json" "$installed")" &&
      [[ "${snapshot[offset]}" == "$slot $pair" && "$rows" == "${snapshot[offset+1]}"$'\n'"${snapshot[offset+2]}" ]] || return 1
  else
    sealed_admit_pair "$slot" "$live" '' yes || return 1
    pair="$(<"$pointer")"
    sealed_authority files "$recovery_receipt/attempt-$slot/release" &&
      cmp -s "$recovery_receipt/attempt-$slot/release" <(printf '%s\n' "${pair%/candidate}") &&
      grep -Fxq "ExecStartPre=/usr/bin/bash $ROOT_DIR/scripts/deploy/quadlet-prestart.sh $slot" "$installed" || return 1
  fi
  cmp -s "$pair/unit.container" "$quadlet_dir/$slot.container" &&
    python3 -ISB "$pair_tool" verify-dropin "$slot" "$pair" "$installed"
}

sealed_recovery_window() {
  python3 -ISB "$ROOT_DIR/scripts/deploy/lib/unstarted-source.py" --window "$service" "$recovery_source" "$revision" ||
    die "현재 감독 프로세스의 복구 요청 권한을 확인할 수 없음: $service"
}

sealed_recovery_check() {
  if [[ "$service" == hololive-api ]]; then
    [[ ! -e "$recovery_receipt/attempt-hololive-alarm-worker" && ! -L "$recovery_receipt/attempt-hololive-alarm-worker" ]] &&
      sealed_recovery_pair "$unchanged_peer" "$peer_generation" yes || return 1
  else
    sealed_recovery_pair "$unchanged_peer" "$peer_generation" no || return 1
  fi
  recovery_witness="$(python3 -ISB "$ROOT_DIR/scripts/deploy/lib/unstarted-source.py" --slot "$recovery_source" "$revision" "$service" "${1:-entry}")" ||
    return 1
  sealed_assert_peer
  [[ "$(sealed_generation "$service")" == "$original_target_generation" ]]
}

sealed_ordinary_transition() {
  python3 -ISB - "$1" "$2" <<'PY'
from pathlib import Path
import sys

transition, window = map(Path, sys.argv[1:])
stages, candidate = window.parent.parent, window.parent.name.removeprefix('dv-tool-')
for path in (transition / 'predecessor.writers', transition / 'attempt-hololive-api',
             transition / 'attempt-hololive-alarm-worker', window,
             *(transition / (phase + '.requested') for phase in ('api', 'worker', 'receipt')),
             *(stages / ('dv-holo-' + slot + '-' + candidate) / 'activation.window' for slot in ('api', 'worker')),
             stages / ('source-' + transition.name.removeprefix('transition-') + '.requested')):
    try:
        path.lstat()
    except FileNotFoundError:
        continue
    sys.exit('ordinary predecessor has recovery evidence')
PY
}

sealed_supersession_delta() {
  python3 -ISB - "$1" "$2" <<'PYTHON'
import subprocess
import sys

repo, candidate = sys.argv[1:]
def git(*args):
    return subprocess.check_output(['git', '-C', repo, *args])
parents = git('show','-s','--format=%P',candidate).decode().split()
parent, = parents
inherited = 'scripts/deploy/sealed-app-case.sh'
entry = b'100755 blob b71da946136468f108196dd66b55e208616a766b\t' + inherited.encode() + b'\n'
if git('ls-tree',parent,'--',inherited) != entry or git('ls-tree',candidate,'--',inherited) != entry:
    sys.exit('changed inherited sealed test')
wanted = {}
for mode, names in [
    ('100644 100644 M', ['CHANGELOG.md','VERSION','deploy/quadlet/README.md','hololive/hololive-api/VERSION','hololive/hololive-alarm-worker/VERSION']),
    ('100755 100755 M', ['scripts/deploy/' + n for n in ('lib/sealed-app.sh','quadlet-redeploy-service.sh','sync-opt-current.sh','sync-opt-current_test.sh')]),
    ('000000 100755 A', ['scripts/deploy/lib/unstarted-source.py','scripts/deploy/unstarted-source-case-host.py']),
    ('000000 100644 A', ['scripts/deploy/' + n for n in ('supersession-window.py','unstarted-source-case.sh','unstarted-source-case-pairs.py','unstarted-source-case-evidence.py')])
]:
    wanted.update(dict.fromkeys(names, mode))
fields = git('diff','--no-renames','--raw','-z',parent,candidate).decode().split('\0')
actual = {}
for header, name in zip(fields[:-1:2],fields[1::2],strict=True):
    old, new, _, _, change = header[1:].split()
    actual[name] = ' '.join([old,new,change])
if actual != wanted:
    sys.exit('unapproved supersession paths or modes')
PYTHON
}

sealed_supersession_path() {
  local status=$1 new_mode=$2 path=$3
  case "$path" in
    scripts/deploy/lib/unstarted-source.py|scripts/deploy/unstarted-source-case-host.py)
      [[ "$status:$new_mode" == A:100755 ]] || { echo "unexpected new supersession executable: $path"; return 1; } ;;
    scripts/deploy/supersession-window.py|scripts/deploy/unstarted-source-case.sh|scripts/deploy/unstarted-source-case-pairs.py|scripts/deploy/unstarted-source-case-evidence.py)
      [[ "$status:$new_mode" == A:100644 ]] || { echo "unexpected new supersession helper mode: $path"; return 1; } ;;
    CHANGELOG.md|VERSION|hololive/hololive-api/VERSION|hololive/hololive-alarm-worker/VERSION|deploy/quadlet/README.md|scripts/deploy/lib/sealed-app.sh|scripts/deploy/quadlet-redeploy-service.sh|scripts/deploy/sealed-app-case.sh|scripts/deploy/sync-opt-current.sh|scripts/deploy/sync-opt-current_test.sh)
      [[ "$status" == M ]] || { echo "supersession requires an existing reviewed path: $path"; return 1; } ;;
    *) echo "unapproved supersession delta: $path"; return 1 ;;
  esac
}

sealed_installed_pair() {
  local service=$1 pair=$2 quadlet=$3
  cmp -s "$pair/unit.container" "$quadlet/$service.container" &&
    cmp -s "$pair/50-worker-profile.conf" "$quadlet/$service.container.d/50-worker-profile.conf" ||
    return 1
}

sealed_manager_action() {
  python3 -ISB "$ROOT_DIR/scripts/deploy/lib/unstarted-source.py" --relay "$1" "$recovery_witness"
}

sealed_reload() {
  if [[ -n "$recovery_receipt" ]]; then
    sealed_manager_action "$1"
  else
    systemctl daemon-reload
  fi
}

sealed_recovery_writers() {
  sealed_check_writer_states "$original_target_generation" "$peer_generation"
}

sealed_check_writer_states() {
  local expected name invocation cid rest state helper
  helper="${tool_root:-${ROOT_DIR:?}}/scripts/deploy/lib/unstarted-source.py"
  for expected in "$@"; do
    read -r name invocation cid rest <<<"$expected"
    [[ -n "$rest" && "$(sealed_generation "$name")" == "$expected" ]] &&
      [[ "$(podman container inspect --format '{{.Id}} {{.Image}} {{.State.Health.Status}}' "$name")" == "$cid ${rest%% *} healthy" ]] &&
      state="$(systemctl show --all "$name.service" -p ActiveState -p InvocationID -p Job -p NRestarts)" &&
      [[ "$(LC_ALL=C sort <<<"$state")" == $'ActiveState=active\nInvocationID='"$invocation"$'\nJob=\nNRestarts=0' ]] ||
      die "복구 writer의 실행 상태가 바뀌었습니다: $name"
  done
  python3 -ISB "$helper" --idle-writers || die "복구 writer 검사 중 manager job이 추가됐습니다"
}

sealed_peer_admission() {
  : "${store:?}" "${revision:?}"
  local dir old tool running expected='' target peer
  recovery_receipt=
  local -a receipts=("$store"/transition-*-"$revision"/revisions) writers
  [[ ${#receipts[@]} == 1 ]] && sealed_authority files "${receipts[0]}" || return 1
  dir="${receipts[0]%/revisions}"; old="${dir##*/transition-}"; old="${old%-"$revision"}"
  tool="$(sed -n 's/^tool=//p' "${receipts[0]}")"
  [[ "$old" =~ ^[0-9a-f]{40}$ && "$tool" =~ ^[0-9a-f]{40}$ ]] || return 1
  if cmp -s "${receipts[0]}" <(printf 'installed=%s\ncandidate=%s\ntool=%s\n' "$old" "$revision" "$tool"); then
    sealed_ordinary_transition "$dir" "$prefix/opt/hololive-bot/release-sources/dv-tool-$revision/source.window" || return 1
    if [[ "$unchanged_peer" == hololive-alarm-worker ]]; then expected="$old"; fi
    sealed_admit_pair "$unchanged_peer" "$peer_generation" "$expected" yes
    return
  fi
  running="$(sed -n 's/^running=//p' "${receipts[0]}")"
  [[ "$running" =~ ^[0-9a-f]{40}$ && "$running" != "$old" && "$running" != "$revision" ]] &&
    cmp -s "${receipts[0]}" <(printf 'installed=%s\ncandidate=%s\ntool=%s\nrunning=%s\n' "$old" "$revision" "$tool" "$running") &&
    sealed_authority files "$dir/predecessor.writers" || return 1
  mapfile -t writers <"$dir/predecessor.writers" || return 1
  [[ ${#writers[@]} == 2 ]] && cmp -s "$dir/predecessor.writers" <(printf '%s\n' "${writers[@]}") || return 1
  local shape=' [0-9a-f]{32} [0-9a-f]{64} [0-9a-f]{64} holo-pgbouncer\|6432$'
  [[ "${writers[0]}" =~ ^hololive-api$shape && "${writers[1]}" =~ ^hololive-alarm-worker$shape ]] || return 1
  recovery_receipt="$dir"
  recovery_source="$old"
  recovery_writer_revision="$running"
  [[ ! -e "$dir/attempt-$service" && ! -L "$dir/attempt-$service" ]] || die "이미 사용된 복구 슬롯; 재시도 금지: $service"
  if [[ "$service" == hololive-api ]]; then
    target="${writers[0]}"; peer="${writers[1]}"
    [[ "$peer_generation" == "$peer" ]] || return 1
  else
    target="${writers[1]}"
  fi
  [[ "$original_target_generation" == "$target" ]] && sealed_recovery_images "$dir" "$1" &&
    sealed_recovery_pair "$service" "$target" yes && sealed_recovery_check
}

sealed_write_intent() {
  : "${deploy_root:?}" "${service:?}" "${quadlet_dir:?}" "${release:?}"
  local pointer="$deploy_root/artifacts/worker-profile/active/$service.pair" installed="$quadlet_dir/$service.container.d/50-worker-profile.conf"
  install -m0444 "$pointer" "$release/before.pair"
  install -m0444 "$installed" "$release/before.conf"
  if [[ -n "${recovery_receipt:-}" ]]; then
    python3 -ISB "$ROOT_DIR/scripts/deploy/lib/unstarted-source.py" --before-pair "$recovery_receipt/pairs.after" "$release" "$service" || return 1
  fi
  printf '%s\n' "$original_target_generation" >"$release/before.generation"
  (cd "$release" && sha256sum before.pair before.conf before.generation previous/pair.json candidate/pair.json candidate/50-worker-profile.conf >activation.intent)
  sync -f "$release"
}

sealed_single_job() {
  local operation="$1" unit="$2" image="$3" record="$4" dependencies dep transaction line job='' type='' rc=0 completion
  local step=writer state query
  [[ "$operation" != start ]] || step=schema
  if [[ -n "${recovery_receipt:-}" ]]; then
    sealed_recovery_check "$step" || die "복구 승인 증거 또는 writer 세대가 변경됨: $unit"
  fi
  dependencies="$(systemctl show "$unit" -p Requires -p Wants -p Requisite -p BindsTo --value)" || return 1
  [[ -n "${dependencies//[[:space:]]/}" ]] || die "$unit dependency inventory missing"
  for dep in $dependencies; do
    query=$dep
    [[ $dep != -.mount ]] || query=/
    if [[ $query == -* ]]; then state="$(systemctl show -p Job --value -- "$query")"; else state="$(systemctl show "$query" -p Job --value)"; fi &&
      systemctl is-active --quiet -- "$dep" && [[ -z "$state" ]] ||
      die "required existing dependency is inactive or has a pending job: $dep"
  done
  sealed_assert_peer
  mkdir -m0700 -- "$record" || die "prior job intent exists: $record; do not retry"
  if [[ -n "${recovery_receipt:-}" && "$operation" == start ]]; then
    python3 -ISB "$ROOT_DIR/scripts/deploy/lib/unstarted-source.py" --overlay >"$record/migrator-overlay"
  fi
  job_since="$EPOCHREALTIME"
  printf '%s %s %s %s\n' "$operation" "$unit" "$image" "$job_since" >"$record/request"
  sync -f "$record"
  if [[ -n "${5:-}" ]]; then printf -v "$5" '%s' true; fi
  if [[ -n "${recovery_receipt:-}" ]]; then
    transaction="$(sealed_manager_action "$step")" || rc=$?
  else
    transaction="$(LC_ALL=C SYSTEMD_LOG_COLOR=0 SYSTEMD_LOG_LEVEL=info SYSTEMD_LOG_TARGET=console \
      systemctl --show-transaction --job-mode=ignore-dependencies "$operation" "$unit" 2>&1)" || rc=$?
  fi
  while IFS= read -r line; do
    [[ "$line" == 'Enqueued anchor job '* ]] || continue
    [[ -z "$job" && "$line" =~ ^Enqueued\ anchor\ job\ ([1-9][0-9]*)\ (.*)/(start|restart|try-restart)\.$ &&
      "${BASH_REMATCH[2]}" == "$unit" ]] || die "[OUTCOME] outcome_unknown: ambiguous $unit job"
    job="${BASH_REMATCH[1]}" type="${BASH_REMATCH[3]}"
  done <<<"$transaction"
  [[ -n "$job" && ( "$type" == "$operation" || ( "$operation" == try-restart && "$type" == restart ) ) ]] ||
    die "[OUTCOME] outcome_unknown: $unit job identity missing"
  printf '%s %s %s\n' "$job" "$type" "$rc" >"$record/anchor"
  sync -f "$record"
  [[ "$rc" == 0 ]] || die "[OUTCOME] outcome_unknown: inspect $unit job $job; no automatic retry"
  completion="$(journalctl --boot --quiet --no-pager -o json --since="@$job_since" --output-fields=INVOCATION_ID \
    _PID=1 "UNIT=$unit" "JOB_ID=$job" JOB_RESULT=done MESSAGE_ID=39f53479d3a045ac8e11786248231fbf)"
  job_invocation="$(python3 -ISB -c 'import json,re,sys
i=json.load(sys.stdin)["INVOCATION_ID"]
if not re.fullmatch("[0-9a-f]{32}",i) or not int(i,16): sys.exit(1)
print(i)' <<<"$completion")" || die "[OUTCOME] outcome_unknown: $unit manager completion missing"
  printf '%s\n' "$job_invocation" >"$record/invocation"
  sync -f "$record"
  if [[ -n "${recovery_receipt:-}" && "$operation" == try-restart ]]; then
    python3 -ISB "$ROOT_DIR/scripts/deploy/lib/unstarted-source.py" --slot "$recovery_source" "$revision" "$service" writer-complete ||
      die "[OUTCOME] outcome_unknown: unconfirmed writer job history: $unit"
  fi
}

sealed_intent_slot() {
  : "${releases:?}" "${pair_tool:?}"
  local dir="$1" service="$2" current="$3" overlay="$4" hashes before
  hashes="$(cd "$dir" && sha256sum before.pair before.conf before.generation previous/pair.json candidate/pair.json candidate/50-worker-profile.conf)" &&
    cmp -s "$dir/activation.intent" <(printf '%s\n' "$hashes") || return 1
  before="$(<"$dir/before.pair")"
  [[ "$before" == "$releases/"* && "$(realpath -e -- "$before")" == "$before" ]] &&
    cmp -s "$dir/before.pair" <(printf '%s\n' "$before") && sealed_authority tree "$before" &&
    cmp -s "$dir/before.conf" "$before/50-worker-profile.conf" &&
    python3 "$pair_tool" verify-dropin "$service" "$before" "$dir/before.conf" &&
    python3 "$pair_tool" verify-dropin "$service" "$dir/candidate" || return 1
  if cmp -s "$current" "$dir/before.pair"; then
    cmp -s "$overlay" "$dir/before.conf" || cmp -s "$overlay" "$dir/candidate/50-worker-profile.conf"
  else
    cmp -s "$current" <(printf '%s/candidate\n' "$dir") &&
      { cmp -s "$overlay" "$dir/candidate/50-worker-profile.conf" ||
        { [[ -e "$dir/restore-original" ]] && cmp -s "$overlay" "$dir/before.conf"; }; }
  fi
}

sealed_recovery_prestart() {
  : "${store:?}"
  local pair="$1" pin generation script lib original
  pin="$(python3 -ISB -c 'import json,sys;print(json.load(open(sys.argv[1])).get("source_generation",""))' "$pair/pair.json")" || return 1
  if [[ -n "$pin" ]]; then
    [[ "$pin" =~ ^[0-9a-f]{40}$ ]] && sealed_authority tree "$store/$pin" &&
      cmp -s "$store/$pin/.source-revision" <(printf '%s\n' "$pin") && printf '%s\n' "$store/$pin"
  else
    generation="$(<"$deploy_root/.source-revision")"; [[ "$generation" =~ ^[0-9a-f]{40}$ ]] || return 1
    original="$store/$generation/scripts/deploy"
    script="$prefix/usr/local/libexec/hololive-bot"
    sealed_authority files "$original/quadlet-prestart.sh" "$script/quadlet-prestart" &&
      cmp -s "$original/quadlet-prestart.sh" "$script/quadlet-prestart" || return 1
    for lib in admin-bind public-bind-mounts postgres-capacity compose-env postgres-endpoint; do
      sealed_authority files "$original/lib/$lib.sh" "$script/lib/$lib.sh" &&
        cmp -s "$original/lib/$lib.sh" "$script/lib/$lib.sh" || return 1
    done
    for lib in collector-slots.sh collector-slots.tsv; do
      if [[ -e "$original/lib/$lib" || -L "$original/lib/$lib" ]]; then
        sealed_authority files "$original/lib/$lib" "$script/lib/$lib" &&
          cmp -s "$original/lib/$lib" "$script/lib/$lib" || return 1
      else
        [[ ! -e "$script/lib/$lib" && ! -L "$script/lib/$lib" ]] || return 1
      fi
    done
    printf 'installed\n'
  fi
}

sealed_check_units() {
  : "${quadlet_dir:?}" "${compose_env:?}" "${deploy_root:?}" "${pair_tool:?}" "${releases:?}" "${mode:?}"
  local service="$1" name=hololive-db-migrate base old current path match image sha values info
  local mcs_pattern=$'^\\[Container\\]\nSecurityLabelLevel=s[0-9]+:c[0-9]+(,c[0-9]+)*$'
  base="$quadlet_dir/$name.container"
  sealed_authority files "$base" "$quadlet_dir/$service.container" || return 1
  if [[ -e "$base.d" || -L "$base.d" ]]; then sealed_authority directory "$base.d" || return 1; fi
  [[ -f "$quadlet_dir/$service.container.d/50-worker-profile.conf" ]] ||
    die 'sealed cut requires the accepted installed profile pair'
  old=d1f13b2f14f7f17b34cd6b3422fff5e0696e8ec53f028bc274723d373d6c4e67
  [[ -f "$base" && ! -L "$base" ]] && { cmp -s "$base" "$ROOT_DIR/deploy/quadlet/hololive-bot/$name.container" ||
    [[ "$(sha256sum -- "$base")" == "$old  $base" ]]; } || die 'unreviewed installed schema migrator'
  cmp -s "$quadlet_dir/$service.container" "$ROOT_DIR/deploy/quadlet/hololive-bot/$service.container" || die 'installed app base differs from candidate'
  for path in "$base.d"/*.conf "$quadlet_dir/$service.container.d"/*.conf; do
    [[ -e "$path" || -L "$path" ]] || continue
    sealed_authority files "$path" || return 1
    [[ -f "$path" && ! -L "$path" ]] || die 'non-regular app unit overlay'
    if [[ "${path##*/}" != 50-worker-profile.conf && "${path##*/}" != 50-migrator-image.conf &&
      "$(awk '!/^([[:space:]]*([#;]|$))/' "$path")" =~ $mcs_pattern ]]; then continue; fi
    case "$path" in
      "$base.d/50-migrator-image.conf")
        image="$(sed -n 's/^Image=//p' "$path")"
        [[ "$image" =~ ^sha256:[0-9a-f]{64}$ ]] || die 'ambiguous installed migrator image'
        if [[ "$(awk '!/^([[:space:]]*([#;]|$))/' "$path")" == $'[Container]\nImage='"$image" ]]; then continue; fi
        sha="$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")"
        [[ "$sha" =~ ^[0-9a-f]{40}$ ]] && cmp -s "$path" <(sealed_migrator_dropin "$store/$sha" "$image") || die 'unreviewed migrator overlay'
        ;;
      "$quadlet_dir/$service.container.d/50-worker-profile.conf")
        current="$deploy_root/artifacts/worker-profile/active/$service.pair"
        sealed_authority files "$current" || return 1
        if [[ -n "${2:-}" ]]; then
          sealed_intent_slot "$2" "$service" "$current" "$path" || die 'profile installation differs from the retained activation intent'
          continue
        fi
        [[ -f "$current" && ! -L "$current" ]] || die 'missing retained active app pointer'
        match="$(<"$current")"
        [[ "$match" == "$releases/"* && "$(realpath -e -- "$match")" == "$match" ]] &&
          sealed_authority tree "$match" && cmp -s "$match/50-worker-profile.conf" "$path" || die 'unrecognized installed profile overlay'
        python3 "$pair_tool" verify-dropin "$service" "$match" "$path" || return 1
        if [[ "$mode" == deploy ]]; then python3 "$pair_tool" verify-live "$service" "$match" "$service" || return 1; fi
        ;;
      *)
        local -a files=("$ROOT_DIR"/deploy/quadlet/optional/*/"$service.container.d/${path##*/}")
        [[ "$path" == "$quadlet_dir/$service.container.d/"* && ${#files[@]} == 1 ]] && cmp -s "$path" "${files[0]}" || die 'unrecognized app unit overlay'
        ;;
    esac
  done
  values="$(postgres_quadlet_environment "$base" "$compose_env" "$base" PGHOST PGPORT PGDATABASE PGUSER PGSSLMODE)" || return 1
  [[ "$values" == $'PGHOST=/var/run/postgresql\nPGPORT=5432\nPGDATABASE=hololive\nPGUSER=hololive_migrator\nPGSSLMODE=disable' ]] || die 'unreviewed schema check endpoint'
  info="$(systemctl show --all "$name.service" -p LoadState -p ActiveState -p SubState -p Job)" || return 1
  [[ "$(LC_ALL=C sort <<<"$info")" == $'ActiveState=inactive\nJob=\nLoadState=loaded\nSubState=dead' ]] || die 'schema check unit not idle'
}

sealed_accept() {
  local service="$1" id="$2" live cid image invocation name endpoint
  live="$(sealed_generation "$service")" || return 1
  read -r name invocation cid image endpoint <<<"$live"
  [[ "$name" == "$service" && "sha256:$image" == "$id" && "$invocation" == "$job_invocation" && -n "$endpoint" ]] || die 'unexpected writer invocation or image'
  if [[ -n "${original_target_generation:-}" ]]; then
    local prior prior_id prior_cid rest
    read -r prior prior_id prior_cid rest <<<"$original_target_generation"
    [[ "$prior" == "$service" && -n "$rest" && "$invocation" != "$prior_id" && "$cid" != "$prior_cid" ]] ||
      die 'writer was not replaced'
  fi
  sealed_ready "$service" "$cid" || return 1
  [[ "$(sealed_generation "$service")" == "$live" ]] || die 'writer changed during H3 acceptance'
  if [[ -n "${3:-}" ]]; then printf -v "$3" '%s' "$live"; fi
}

sealed_ready() {
  case "$1" in
    hololive-api) podman exec "$2" ./bin/healthcheck --api-key-env API_SECRET_KEY https://127.0.0.1:30001/internal/ready https://127.0.0.1:30006/health ;;
    hololive-alarm-worker) podman exec "$2" ./bin/healthcheck --api-key-env API_SECRET_KEY https://127.0.0.1:30007/internal/ready ;;
    *) return 1 ;;
  esac
}

sealed_loaded() {
  local name="$1" image="$2" fragment
  fragment="$(systemctl show "$name.service" --property=FragmentPath --value)" || return 1
  [[ "$fragment" == "$prefix/run/systemd/generator/$name.service" && -f "$fragment" && ! -L "$fragment" ]] ||
    die "unrecognized generated app unit: $name"
  python3 -ISB - "$fragment" "$name" "${3:-$ROOT_DIR}" "$image" <<'PY'
from pathlib import Path
import shlex
import sys

fragment, name, source, image = sys.argv[1:]
settings = {'ExecStartPre': [], 'ExecStart': []}
section = ''
for line in Path(fragment).read_text().splitlines():
    if line.startswith('['):
        section = line
    key, _, value = line.partition('=')
    if section == '[Service]' and key in settings:
        if value:
            settings[key].append(shlex.split(value))
        else:
            settings[key].clear()
pre = (['/usr/local/libexec/hololive-bot/quadlet-prestart', name] if source == 'installed'
       else ['/usr/bin/bash', source + '/scripts/deploy/quadlet-prestart.sh', name])
migrator = name == 'hololive-db-migrate'
if migrator:
    pre.append('schema')
if settings['ExecStartPre'] != [pre] or len(settings['ExecStart']) != 1:
    sys.exit('unverified generated prestart or command')
command, = settings['ExecStart']
tail = [image, '--check'] if migrator else [image]
if command[-len(tail):] != tail or command.count(image) != 1:
    sys.exit('generated app image or execution mode mismatch')
if migrator:
    if (not any('hololive-bot_holo-pg-admin-socket:/var/run/postgresql:ro' in arg for arg in command)
            or any('/migrations' in arg or 'MIGRATION_' in arg for arg in command)):
        sys.exit('generated migrator uses unexpected volumes or legacy environment')
PY
}

sealed_schema_evidence() {
  local since=$1 invocation=$2 image=$3 proof cid event when started action format reference
  [[ $invocation =~ ^[0-9a-f]{32}$ && $image =~ ^sha256:[0-9a-f]{64}$ && $since =~ ^[0-9]+\.[0-9]{6}$ ]] || return 1
  proof="$(journalctl --boot --quiet --no-pager -o json --since="@$since" \
    --output-fields=CONTAINER_ID_FULL,MESSAGE --grep='^==> hololive migrations applied ' \
    "_SYSTEMD_INVOCATION_ID=$invocation" CONTAINER_NAME=hololive-db-migrate)" || return 1
  cid="$(python3 -ISB -c 'import json,re,sys
row=json.load(sys.stdin)
if (not re.fullmatch(r"==> hololive migrations applied \(applied=0 skipped=([1-9][0-9]*) total=\1\)",row["MESSAGE"])
        or not re.fullmatch("[0-9a-f]{64}",row["CONTAINER_ID_FULL"])): sys.exit(1)
print(row["CONTAINER_ID_FULL"])' <<<"$proof")" || return 1
  for action in start died; do
    if [[ $action == start ]]; then
      format="{{if or (eq .Image \"$image\") (eq .Image \"${image#sha256:}\")}}{{.ID}}|{{.Name}}|{{.TimeNano}}|0{{else}}{{.ID}}|{{.Name}}|{{.TimeNano}}|image={{.Image}}{{end}}"
    else
      format='{{.ID}}|{{.Name}}|{{.TimeNano}}|{{.ContainerExitCode}}'
    fi
    event="$(podman events --stream=false --since "${since%.*}" --filter type=container --filter "container=$cid" \
      --filter "event=$action" --format "$format")" || return 1
    if [[ $action == start && "$event" =~ ^$cid\|hololive-db-migrate\|([1-9][0-9]{18})\|image=([^[:space:]\|]*)$ ]]; then
      when=${BASH_REMATCH[1]} reference=${BASH_REMATCH[2]}
      [[ ! $reference =~ ^(sha256:)?[0-9a-f]{64}$ ]] || return 1
      proof="$(journalctl --boot --quiet --no-pager -o json --since="@$since" --output-fields=_CMDLINE \
        _UID=0 _COMM=podman _SYSTEMD_UNIT=hololive-db-migrate.service "_SYSTEMD_INVOCATION_ID=$invocation" \
        PODMAN_NAME=hololive-db-migrate PODMAN_EVENT=start "PODMAN_ID=$cid")" || return 1
      python3 -ISB -c 'import json,shlex,sys
argv=shlex.split(json.load(sys.stdin)["_CMDLINE"])
if argv[:2] != ["/usr/bin/podman","run"] or argv[-2:] != [sys.argv[1],"--check"]: sys.exit(1)' "$image" <<<"$proof" || return 1
      event="$cid|hololive-db-migrate|$when|0"
    fi
    [[ "$event" =~ ^$cid\|hololive-db-migrate\|([1-9][0-9]{18})\|0$ ]] || return 1
    when=${BASH_REMATCH[1]}
    (( when >= ${since/./}000 )) || return 1
    if [[ $action == start ]]; then started=$when; else (( when >= started )) || return 1; fi
  done
  printf '%s %s\n' "$cid" "$when"
}

sealed_check_result() {
  local evidence
  evidence="$(sealed_schema_evidence "$1" "$job_invocation" "$migrator_id")" || return 1
  printf '%s %s\n' "${evidence%% *}" "$migrator_id"
}
