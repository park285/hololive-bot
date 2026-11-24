#!/usr/bin/env bash
set -Eeuo pipefail

die() { echo "[quadlet-redeploy] $*" >&2; exit 1; }
usage() {
  echo "Usage: $0 hololive-api <api-image>" >&2
  echo "       $0 hololive-alarm-worker <alarm-worker-image> <api-image-for-migration>" >&2
  echo "       $0 --quiesce <api-image> <alarm-worker-image>" >&2
  echo "       $0 --resume <api-release-dir> <alarm-worker-release-dir>" >&2
  echo "Images are sha256:<id> (build host podman image inspect .Id) or a loaded reference." >&2
  exit 2
}

mode=deploy
declare -A candidate_refs=() candidate_ids=() release_paths=() original_invocations=()
declare -A original_cids=() opened_invocations=() opened_cids=()
declare -A unmasked=()
declare -A failed_start_jobs=() failed_start_times=()
case "${1:-}" in
  hololive-api)
    [[ $# -eq 2 ]] || usage
    services=(hololive-api)
    candidate_refs[hololive-api]="$2"
    migrator_ref="$2"
    ;;
  hololive-alarm-worker)
    [[ $# -eq 3 ]] || usage
    services=(hololive-alarm-worker)
    candidate_refs[hololive-alarm-worker]="$2"
    migrator_ref="$3"
    ;;
  --quiesce)
    [[ $# -eq 3 ]] || usage
    mode=quiesce
    services=(hololive-alarm-worker hololive-api)
    candidate_refs[hololive-api]="$2"
    candidate_refs[hololive-alarm-worker]="$3"
    migrator_ref="$2"
    ;;
  --resume)
    [[ $# -eq 3 ]] || usage
    mode=resume
    services=(hololive-alarm-worker hololive-api)
    release_paths[hololive-api]="$2"
    release_paths[hololive-alarm-worker]="$3"
    ;;
  *) usage ;;
esac
service="${services[0]}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  [[ -z "${HOLOLIVE_QUADLET_TEST_ROOT:-}" ]] || die "HOLOLIVE_QUADLET_TEST_ROOT is only for non-root tests"
  prefix=""
else
  prefix="${HOLOLIVE_QUADLET_TEST_ROOT:-}"
  [[ -n "$prefix" ]] || die "must run as root (sudo)"
fi
deploy_root="$(realpath -m -- "$prefix/opt/hololive-bot/compose/current")"
quadlet_dir="$prefix/etc/containers/systemd/hololive-bot"
compose_env="$prefix/etc/stack-secrets/hololive-bot/compose.env"
fence_dir="$prefix/var/lib/hololive-postgres-fence"
source_stamp="$deploy_root/.source-revision"
if [[ -z "$prefix" && "$ROOT_DIR" != "$deploy_root" ]]; then
  die "run the deployed copy: $deploy_root/scripts/deploy/quadlet-redeploy-service.sh"
fi
pair_tool="$ROOT_DIR/scripts/deploy/lib/worker-profile-pair.py"
guard_tool="$ROOT_DIR/scripts/deploy/lib/systemd-stop-guard.py"
releases="$deploy_root/artifacts/worker-profile/releases"
owner_marker="$releases/.central-writer-hold"
lock_dir="$prefix/run/lock"
install -d -m 0755 "$lock_dir"
exec 9>"$lock_dir/hololive-quadlet-redeploy.lock"
flock -n 9 || die "another central Quadlet writer is running"
# shellcheck source=scripts/deploy/lib/postgres-capacity.sh
. "$ROOT_DIR/scripts/deploy/lib/postgres-capacity.sh"
. "$ROOT_DIR/scripts/deploy/lib/compose-env.sh"
. "$ROOT_DIR/scripts/deploy/lib/postgres-endpoint.sh"
mask_dir="$prefix/etc/systemd/system"
sync_paths() {
  python3 - "$@" <<'PY'
import os
import sys

for path in sys.argv[1:]:
    fd = os.open(path, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
PY
}
opened=()
opening_owned=false
defer_signal=false
pending_signal=
cancel() {
  if [[ "$defer_signal" == true ]]; then
    pending_signal="${pending_signal:-$1}"
  else
    trap '' INT TERM HUP
    exit "$1"
  fi
}
end_defer() {
  defer_signal=false
  [[ -z "$pending_signal" ]] || exit "$pending_signal"
}

stage=preflight
release=""
activation_started=false
quiescence_started=false
on_exit() {
  trap '' INT TERM HUP
  local status="${pending_signal:-$1}"
  trap - EXIT
  [[ "$status" -ne 0 ]] || return 0
  if [[ "$opening_owned" == true ]]; then
    if (restore_opened_hold); then
      echo "[HOLD] only the units opened by this resume were stopped and re-masked" >&2
    else
      echo "[HOLD] recovery unconfirmed; inspect both writer units without retrying" >&2
    fi
  fi
  if [[ "$activation_started" == true ]]; then
    if [[ "$mode" == deploy ]]; then
      echo "[OUTCOME] failed_after_activation service=${service[0]} stage=$stage release=$release: the candidate pair stays installed; roll back only after schema/admission compatibility is approved: scripts/deploy/quadlet-rollback-service.sh $release --check, then --apply" >&2
    else
      echo "[OUTCOME] failed_after_activation services=${services[*]} stage=$stage releases=${release_paths[*]:-none}: retain all pairs and inspect unit state before an approved rollback" >&2
    fi
  elif [[ "$quiescence_started" == true ]]; then
    echo "[OUTCOME] quiescence_failed services=${services[*]} stage=$stage releases=${release_paths[*]:-none}: the hold may be partial; inspect both units and do not unmask automatically" >&2
  else
    echo "[OUTCOME] not_activated service=${service[0]} stage=$stage release=${release:-none}: the candidate pair was not installed" >&2
  fi
  exit "$status"
}
trap 'on_exit $?' EXIT
trap 'cancel 130' INT
trap 'cancel 143' TERM
trap 'cancel 129' HUP

if [[ "$(hostname -s)" == kapu && "${HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED:-}" != 1 ]]; then
  die "refusing to redeploy central units on build-control host kapu"
fi
assert_no_fence_marker() {
  local marker
  for marker in fence.intent fenced; do
    [[ ! -e "$fence_dir/$marker" ]] || die "primary fence marker present: $fence_dir/$marker"
  done
}
assert_no_fence_marker
mask_path() { printf '%s/%s.service' "$mask_dir" "$1"; }
mask_anchor() { local dir="${release_paths[$1]}"; printf '%s/.%s.mask' "$mask_dir" "${dir##*/}"; }
anchor_identity() {
  local anchor recorded="${release_paths[$1]}/mask.identity" identity
  anchor="$(mask_anchor "$1")" && [[ -f "$recorded" && ! -L "$recorded" && -L "$anchor" &&
    "$(readlink -- "$anchor")" == /dev/null ]] || return 1
  identity="$(stat -c '%d:%i' -- "$anchor")" &&
    cmp -s "$recorded" <(printf '%s\n' "$identity") && printf '%s\n' "$identity"
}
guard() {
  local identity
  identity="$(anchor_identity "$2")" || return 1
  python3 -I -B "$guard_tool" "$1" "$2.service" "$(mask_anchor "$2")" "$identity"
}
owned_mask() {
  local path owner installed
  path="$(mask_path "$1")"
  [[ -L "$path" ]] || return 1
  owner="$(anchor_identity "$1")" && installed="$(stat -c '%d:%i' -- "$path")" &&
    [[ "$owner" == "$installed" ]]
}
assert_stopped() {
  local name="$1" unit="$1.service" job
  owned_mask "$name" &&
    [[ "$(systemctl show "$unit" -p LoadState --value)" == masked &&
      "$(systemctl show "$unit" -p UnitFileState --value)" == masked &&
      "$(systemctl show "$unit" -p ActiveState --value)" == inactive &&
      "$(systemctl show "$unit" -p MainPID --value)" == 0 &&
      "$(systemctl show "$unit" -p ControlPID --value)" == 0 &&
      "$(systemctl show "$unit" -p Result --value)" == success ]] || return 1
  job="$(systemctl show "$unit" -p Job --value)" && [[ -z "$job" ]] && container_absent "$name" &&
    container_absent "${original_cids[$name]}"
}
assert_coordinated_hold() {
  local name
  for name in "${services[@]}"; do
    assert_stopped "$name" || die "$name: owned persistent writer hold not confirmed"
  done
}
container_absent() {
  local rc=0
  podman container exists "$1" || rc=$?
  [[ "$rc" == 1 ]]
}
owns_hold() {
  local anchor="${release_paths[hololive-api]}/captured.invocation"
  [[ -f "$anchor" && ! -L "$anchor" && -f "$owner_marker" && ! -L "$owner_marker" && "$anchor" -ef "$owner_marker" ]]
}
held_stop() {
  local dir="${release_paths[$1]}" stop=stop
  if [[ -e "$dir/last-stop" || -L "$dir/last-stop" ]]; then
    [[ -f "$dir/last-stop" && ! -L "$dir/last-stop" ]] || return 1
    stop="$(<"$dir/last-stop")"
    [[ "$stop" =~ ^rehold\.[[:alnum:]]{6}$ ]] || return 1
  fi
  printf '%s\n' "$stop"
}
held_cid() {
  local stop at invocation cid
  stop="$(held_stop "$1")" && read -r at invocation cid <"${release_paths[$1]}/$stop/request" &&
    [[ "$at" =~ ^@[0-9]+\.[0-9]{6}$ && "$invocation" =~ ^[0-9a-f]{32}$ && "$cid" =~ ^[0-9a-f]{64}$ ]] &&
    printf '%s\n' "$cid"
}
active_invocation() {
  local unit="$1.service" state id
  state="$(systemctl show "$unit" -p ActiveState --value)" && [[ "$state" == active ]] &&
    id="$(systemctl show "$unit" -p InvocationID --value)" &&
    [[ "$id" =~ ^[0-9a-f]{32}$ && "$id" != 00000000000000000000000000000000 ]] &&
    printf '%s\n' "$id"
}
managed_container() {
  local name="$1" metadata running cid conmon
  metadata="$(podman inspect -f '{{.State.Running}} {{.Id}} {{.State.ConmonPid}}' "$name")" || return 1
  read -r running cid conmon <<<"$metadata"
  [[ "$running" == true && "$cid" =~ ^[0-9a-f]{64}$ && "$conmon" =~ ^[1-9][0-9]{0,9}$ &&
    "$(systemctl show "$name.service" -p MainPID --value)" == "$conmon" ]] || return 1
  printf '%s %s\n' "$cid" "$conmon"
}
first_generation() {
  local name="$1" invocation container confirm restarts
  restarts="$(systemctl show "$name.service" -p NRestarts --value)" && [[ "$restarts" == 0 ]] &&
    invocation="$(active_invocation "$name")" && container="$(managed_container "$name")" &&
    [[ "$(active_invocation "$name")" == "$invocation" ]] &&
    confirm="$(managed_container "$name")" && [[ "$container" == "$confirm" ]] &&
    restarts="$(systemctl show "$name.service" -p NRestarts --value)" && [[ "$restarts" == 0 ]] &&
    printf '%s %s\n' "$invocation" "${container%% *}"
}
request_start() {
  local LC_ALL=C unit="$1" transaction line job='' client=0
  transaction="$(LC_ALL=C SYSTEMD_LOG_COLOR=0 SYSTEMD_LOG_LEVEL=info SYSTEMD_LOG_TARGET=console \
    systemctl --show-transaction start "$unit" 2>&1)" || client=$?
  while IFS= read -r line; do
    [[ "$line" == 'Enqueued anchor job '* ]] || continue
    [[ -z "$job" && "$line" =~ ^Enqueued\ anchor\ job\ ([1-9][0-9]*)\ (.*)/start\.$ &&
      "${BASH_REMATCH[2]}" == "$unit" ]] || return 1
    job="${BASH_REMATCH[1]}"
  done <<<"$transaction"
  [[ -n "$job" ]] && printf '%s %s\n' "$job" "$client"
}
started_generation() {
  local name="$1" since="$2" job="$3" wait_for_job="${4:-false}" current limit='' budget record invocation generation
  if [[ "$wait_for_job" == true ]]; then
    while :; do
      current="$(systemctl show "$name.service" -p Job --value)" || return 1
      [[ -n "$current" ]] || break
      [[ "$current" == "$job" ]] || return 1
      if [[ -z "$limit" ]]; then
        budget="$(guard start-budget "$name")" && [[ "$budget" =~ ^[1-9][0-9]*$ ]] || return 1
        limit=$((SECONDS + budget))
      fi
      (( SECONDS < limit )) || return 1
      sleep 0.25 || return 1
    done
  fi
  record="$(journalctl --boot --quiet --no-pager -o json --output-fields=INVOCATION_ID --since="$since" \
    _PID=1 "UNIT=$name.service" "JOB_ID=$job" JOB_TYPE=start JOB_RESULT=done MESSAGE_ID=39f53479d3a045ac8e11786248231fbf)" || return 1
  [[ -n "$record" && "$record" != *$'\n'* ]] || return 1
  invocation="$(python3 -I -B -c 'import json,sys; print(json.load(sys.stdin)["INVOCATION_ID"])' <<<"$record")" || return 1
  generation="$(first_generation "$name")" && [[ "${generation%% *}" == "$invocation" ]] && printf '%s\n' "$generation"
}
terminal_state() {
  systemctl show "$1.service" --all -p LoadState,ActiveState,InvocationID,Result,ExecMainCode,ExecMainStatus,MainPID,ControlPID
}
stop_generation() {
  local name="$1" id="$2" cid="$3" evidence="$4" reholding="${5:-false}" since job=0 inventory=0 observed=true field container LC_ALL=C
  local start_id=de5b426a63be47a7b6ac3eaac82e2f6f done_id=9d1aaa27d60140bd96365438aad20286
  guard check "$name" && container="$(managed_container "$name")" && [[ "${container%% *}" == "$cid" &&
    "$(active_invocation "$name")" == "$id" ]] || return 1
  since="@$EPOCHREALTIME"
  if [[ -z "$evidence" ]] || ! { printf '%s %s %s\n' "$since" "$id" "$cid" >"$evidence/request" &&
      sync_paths "$evidence/request" "$evidence"; }; then
    [[ "$reholding" == true ]] || return 1
    observed=false
  fi
  if [[ "$reholding" == true ]]; then
    [[ "$(first_generation "$name")" == "$id $cid" ]] || return 1
  fi
  systemctl stop "$name.service" || job=$?
  [[ -n "$evidence" ]] || return 1
  printf '%d\n' "$job" >"$evidence/job-status" || return 1
  terminal_state "$name" >"$evidence/terminal" || observed=false
  journalctl --boot --quiet --no-pager -o json --output-fields=INVOCATION_ID,MESSAGE_ID,JOB_ID,JOB_RESULT --since="$since" \
    _PID=1 "UNIT=$name.service" "INVOCATION_ID=$id" JOB_TYPE=stop "MESSAGE_ID=$start_id" "MESSAGE_ID=$done_id" \
    >"$evidence/journal" || observed=false
  python3 -I -B "$guard_tool" exit-status "$name.service" "$cid" "$since" >"$evidence/container-exit" || observed=false
  podman container exists "$name" || inventory=$?
  printf '%d\n' "$inventory" >"$evidence/container-exists" || return 1
  sync_paths "$evidence/"{job-status,terminal,journal,container-exit,container-exists} "$evidence" || return 1
  container_absent "$cid" || return 1
  [[ "$observed" == true && "$job" == 0 && "$inventory" == 1 ]] && cmp -s "$evidence/container-exit" <(printf '0\n') || return 1
  python3 -I -B - "$evidence/journal" "$id" "$start_id" "$done_id" <<'PY' || return 1
import json
import sys

path, invocation, started, stopped = sys.argv[1:]
with open(path) as records:
    first, last = map(json.loads, records)
job = first["JOB_ID"]
sys.exit(not (job.isdecimal() and int(job) > 0 and job == last["JOB_ID"]
              and first["INVOCATION_ID"] == last["INVOCATION_ID"] == invocation
              and first["MESSAGE_ID"] == started and last["MESSAGE_ID"] == stopped and last["JOB_RESULT"] == "done"))
PY
  for field in LoadState=loaded ActiveState=inactive Result=success ExecMainStatus=0 MainPID=0 ControlPID=0; do
    grep -qxF "$field" "$evidence/terminal" || return 1
  done
  grep -qxF 'ExecMainCode=1' "$evidence/terminal" || grep -qxF 'ExecMainCode=0' "$evidence/terminal" || return 1
  grep -qxF "InvocationID=$id" "$evidence/terminal" || grep -qxF 'InvocationID=' "$evidence/terminal" || return 1
}
restore_opened_hold() {
  local name dir path generation failed=false
  local -A armed=() evidence=() clean=() needs_guard=()
  for name in "${opened[@]}"; do
    if owned_mask "$name"; then
      [[ ! -v "unmasked[$name]" ]] && assert_stopped "$name" || failed=true
      continue
    fi
    path="$(mask_path "$name")"
    if [[ -e "$path" || -L "$path" ]]; then failed=true; continue; fi
    if [[ ! -v "unmasked[$name]" && "$(systemctl show "$name.service" -p LoadState --value)" == masked ]] &&
      container_absent "${original_cids[$name]}" && container_absent "$name" && guard undo-unmask "$name"; then
      assert_stopped "$name" || failed=true
      continue
    fi
    if [[ -e "$path" || -L "$path" ]]; then failed=true; continue; fi
    needs_guard[$name]=1
    dir="$(mktemp -d "${release_paths[$name]}/rehold.XXXXXX")" || { failed=true; continue; }
    evidence[$name]="$dir"
    terminal_state "$name" >"$dir/before" && sync_paths "$dir/before" "$dir" "${release_paths[$name]}" || failed=true
  done
  for name in "${opened[@]}"; do
    [[ -v "needs_guard[$name]" ]] || continue
    if guard arm "$name"; then armed[$name]=1; else failed=true; fi
  done
  for name in "${opened[@]}"; do
    [[ -v "armed[$name]" ]] || continue
    if [[ -v "failed_start_jobs[$name]" ]]; then
      failed=true
      generation="$(started_generation "$name" "${failed_start_times[$name]}" "${failed_start_jobs[$name]}" true)" || continue
      opened_invocations[$name]="${generation%% *}"
      opened_cids[$name]="${generation#* }"
    fi
    if [[ -n "${opened_invocations[$name]:-}" && -n "${opened_cids[$name]:-}" ]] &&
      stop_generation "$name" "${opened_invocations[$name]}" "${opened_cids[$name]}" "${evidence[$name]:-}" true; then
      clean[$name]=1
    else
      failed=true
    fi
  done
  for name in "${opened[@]}"; do
    [[ -v "clean[$name]" ]] || continue
    guard seal "$name" || failed=true
  done
  for name in "${services[@]}"; do assert_stopped "$name" || failed=true; done
  [[ "$failed" == false ]] || return 1
  verify_receipt || return 1
  if (( ${#clean[@]} )); then
    for name in "${!clean[@]}"; do
      dir="${evidence[$name]}"
      printf '%s\n' "${dir##*/}" >"${release_paths[$name]}/last-stop" &&
        sync_paths "${release_paths[$name]}/last-stop" "${release_paths[$name]}" || return 1
    done
    publish_receipt || return 1
  fi
  rmdir -- "${release_paths[hololive-api]}/opening" && sync_paths "${release_paths[hololive-api]}"
}
if [[ "$mode" != resume ]]; then
  [[ ! -e "$owner_marker" && ! -L "$owner_marker" ]] || die "ongoing coordinated writer hold"
  for peer in hololive-alarm-worker hololive-api; do
    drop="$mask_dir/$peer.service.d/90-hololive-stop-guard.conf"
    [[ ! -L "$mask_dir/$peer.service" && ! -e "$drop" && ! -L "$drop" ]] || die "$peer has a writer hold"
  done
  for service in "${services[@]}"; do
    [[ -f "$quadlet_dir/$service.container" ]] || die "installed unit missing: $quadlet_dir/$service.container"
    systemctl is-active --quiet "$service.service" ||
      die "$service.service must be running so its loaded profile pair can be retained"
    if [[ "$mode" == quiesce ]]; then
      [[ "$(systemctl show "$service.service" -p LoadState --value)" == loaded &&
        ! -e "$(mask_path "$service")" && ! -L "$(mask_path "$service")" ]] ||
        die "$service.service already has a hold that this operation does not own"
      invocation="$(systemctl show "$service.service" -p InvocationID --value)"
      [[ "$invocation" =~ ^[0-9a-f]{32}$ && "$invocation" != 00000000000000000000000000000000 ]] ||
        die "$service has no running invocation"
      original_invocations[$service]="$invocation"
    fi
  done
fi

[[ -f "$source_stamp" ]] ||
  die "deploy tree revision record missing; run sync-opt-current.sh from the clean checkout first"
revision="$(<"$source_stamp")"
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || die "deploy tree revision is not a full commit: $deploy_root/.source-revision"
host_arch="$(podman info --format '{{.Host.Arch}}')"

resolve_image() {
  local ref="$1" title="$2" status=0 id value
  podman image exists "$ref" || status="$?"
  case "$status" in
    0) ;;
    1) die "image not found in the root store: $ref" ;;
    *) die "image inventory failed for $ref (status=$status)" ;;
  esac
  id="$(podman image inspect --format '{{.Id}}' "$ref")"
  [[ "$id" =~ ^[0-9a-f]{64}$ ]] || die "unexpected image identity for $ref"
  id="sha256:$id"
  value="$(podman image inspect --format '{{.Os}}/{{.Architecture}}' "$id")"
  [[ "$value" == "linux/$host_arch" ]] || die "$ref is $value; this host runs linux/$host_arch"
  value="$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.title"}}' "$id")"
  [[ "$value" == "$title" ]] || die "$ref is not a $title image"
  value="$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$id")"
  [[ "$value" == "$revision" ]] || die "$ref revision label does not match the deploy tree revision $revision"
  printf '%s\n' "$id"
}
load_coordinated_release() {
  local requested="$1" field
  [[ "$requested" == /* ]] || die "release path must be absolute"
  release="$(realpath -e -- "$requested")" || die "release directory not found"
  [[ -d "$release" && "$release" == "$releases/"* ]] || die "release escaped the retained store"
  for field in service candidate.revision candidate.image migrator.image previous.endpoint captured.invocation; do
    [[ -f "$release/$field" && ! -L "$release/$field" ]] || die "retained release metadata is missing: $field"
  done
  cmp -s "$release/service" <(printf '%s\n' "$service") || die "retained release service mismatch"
  cmp -s "$release/candidate.revision" <(printf '%s\n' "$revision") || die "deployment tree changed after quiescence"
  candidate_refs[$service]="$(<"$release/candidate.image")"
  if [[ ! "${candidate_refs[$service]}" =~ ^sha256:[0-9a-f]{64}$ ]] ||
    ! cmp -s "$release/candidate.image" <(printf '%s\n' "${candidate_refs[$service]}"); then
    die "retained candidate identity is invalid"
  fi
  release_paths[$service]="$release"
}

hold_receipt() {
  local name dir invocation inode endpoint stop
  printf '%s\n' card-on-accepted-pool-v1
  for name in "${services[@]}"; do
    dir="${release_paths[$name]}"
    invocation="$(<"$dir/captured.invocation")"
    [[ "$invocation" =~ ^[0-9a-f]{32}$ ]] || return 1
    inode="$(anchor_identity "$name")" || return 1
    endpoint="$(<"$dir/previous.endpoint")"
    [[ -n "$endpoint" && "$endpoint" != *$'\n'* ]] || return 1
    printf '%s %s %s %s %s\n' "$name" "$dir" "$invocation" "$inode" "$endpoint"
    (cd "$dir" && sha256sum previous/pair.json candidate.image migrator.image candidate.revision \
      stop/{request,job-status,terminal,journal,container-exit,container-exists}) || return 1
    stop="$(held_stop "$name")" || return 1
    if [[ "$stop" != stop ]]; then
      (cd "$dir" && sha256sum "$stop"/{request,job-status,terminal,journal,container-exit,container-exists}) || return 1
    fi
    (cd "$dir" && sha256sum infrastructure) || return 1
  done
}
publish_receipt() {
  local dir="${release_paths[hololive-api]}"
  hold_receipt >"$dir/quiesced.new" && mv -T -- "$dir/quiesced.new" "$dir/quiesced" &&
    sync_paths "$dir/quiesced" "$dir"
}
verify_receipt() {
  local receipt="${release_paths[hololive-api]}/quiesced" expected
  owns_hold && [[ -f "$receipt" && ! -L "$receipt" ]] || die "missing owner or invalid quiescence receipt"
  if ! { expected="$(hold_receipt)" && cmp -s "$receipt" <(printf '%s\n' "$expected"); }; then
    die "release pair or mask does not belong to this quiescence receipt"
  fi
}
check_receipt() {
  local opening="${release_paths[hololive-api]}/opening" name
  [[ ! -e "$opening" && ! -L "$opening" ]] || die "previous release opening is incomplete"
  verify_receipt
  for name in "${services[@]}"; do
    original_cids[$name]="$(held_cid "$name")" || die "$name: missing held generation"
  done
  assert_coordinated_hold
}

if [[ "$mode" == resume ]]; then
  for service in "${services[@]}"; do
    load_coordinated_release "${release_paths[$service]}"
  done
  check_receipt
  quiescence_started=true
  migrator_ref="${candidate_refs[hololive-api]}"
fi
infrastructure_generation() {
  local state running cid image started
  state="$(podman container inspect --format '{{.State.Running}} {{.Id}} {{.Image}} {{.State.StartedAt.UnixNano}}' holo-postgres)" || return 1
  read -r running cid image started <<<"$state"
  [[ "$running" == true && "$cid" =~ ^[0-9a-f]{64}$ && "$image" =~ ^[0-9a-f]{64}$ &&
     "$started" =~ ^[1-9][0-9]*$ ]] || return 1
  printf 'holo-postgres %s %s %s\n' "$cid" "$image" "$started"
}
infrastructure="$(infrastructure_generation)" || die "cannot establish the running PostgreSQL generation"
if [[ "$mode" == resume ]]; then
  for service in "${services[@]}"; do
    cmp -s "${release_paths[$service]}/infrastructure" <(printf '%s\n' "$infrastructure") || die "infrastructure changed since the hold"
  done
fi
assert_infrastructure() {
  local actual
  actual="$(infrastructure_generation)" && [[ "$actual" == "$infrastructure" ]] || die "accepted database generation changed"
}
for service in "${services[@]}"; do
  candidate_ids[$service]="$(resolve_image "${candidate_refs[$service]}" "$service")"
done
migrator_id="$(resolve_image "$migrator_ref" hololive-api)"
postgres_capacity_assert_target "$ROOT_DIR" "$compose_env"
assert_writer_endpoint() {
  local unit="$quadlet_dir/$1.container" values
  values="$(postgres_quadlet_environment "$unit" "$compose_env" "$unit" POSTGRES_HOST POSTGRES_PORT POSTGRES_SOCKET_PATH POSTGRES_SSLMODE)"
  [[ "$values" == $'POSTGRES_HOST=holo-postgres\nPOSTGRES_PORT=5432\nPOSTGRES_SOCKET_PATH=\nPOSTGRES_SSLMODE=verify-full' ]] ||
    die "$1: direct TLS endpoint and verify-full are required"
}
require_card_ledger() {
  local result receipts head_query contract
  . "$ROOT_DIR/scripts/deploy/lib/migration-receipts.sh"
  receipts="$(migration_receipts "$ROOT_DIR")" || return 1
  contract="$(migration_target_contract "$ROOT_DIR" "$receipts")" || return 1
  head_query="$(<"$ROOT_DIR/hololive/hololive-api/internal/migrationrunner/queries/lineage_head.sql")" || return 1
  result="$(podman exec -i holo-postgres env 'PGOPTIONS=-c default_transaction_read_only=on -c lock_timeout=3s -c statement_timeout=30s' \
    psql -XqAtw -v ON_ERROR_STOP=1 -h /var/run/postgresql -U postgres_admin -d hololive <<SQL
BEGIN READ ONLY;
PREPARE admitted_head(text) AS
SELECT current_database() = 'hololive' AND current_user = 'postgres_admin' AND inet_server_addr() IS NULL
  AND current_setting('transaction_read_only') = 'on' AND current_setting('default_transaction_read_only') = 'on'
  AND current_setting('lock_timeout') = '3s' AND current_setting('statement_timeout') = '30s'
  AND epoch IN (3,4) AND pending = 0 AND (${contract%;}) FROM (${head_query%;}) AS head;
EXECUTE admitted_head('$receipts');
ROLLBACK;
SQL
  )" && [[ "$result" == t ]] || die "migration lineage not confirmed; no single-writer migration or new writer activation"
}

postgres_capacity_assert_installed "$ROOT_DIR" "$compose_env" "$quadlet_dir"
for service in "${services[@]}"; do assert_writer_endpoint "$service"; done
if [[ "$mode" == deploy ]]; then
  require_card_ledger
  postgres_endpoint_require_same "$(postgres_endpoint_container "$service")" 'holo-postgres|5432' ||
    die "single-writer endpoint change refused; use the coordinated pre-sync hold"
fi

prepare_slot() {
  stage=previous-pair
  install -d -m 0755 "$releases"
  release="$(mktemp -d "$releases/$service-v2-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")"
  release_paths[$service]="$release"
  chmod 0755 "$release"
  printf '%s\n' "$service" >"$release/service"
  printf '%s\n' "$revision" >"$release/candidate.revision"
  printf '%s\n' "${candidate_ids[$service]}" >"$release/candidate.image"
  printf '%s\n' "$migrator_id" >"$release/migrator.image"
  if [[ "$mode" == quiesce ]]; then
    printf '%s\n' "${original_invocations[$service]}" >"$release/captured.invocation"
    printf '%s\n' "$infrastructure" >"$release/infrastructure"
  fi
  if [[ -e "$quadlet_dir/$service.container.d/50-worker-profile.conf" ]]; then
    python3 "$pair_tool" capture "$service" "$release/previous" "$service" "$deploy_root"
  else
    python3 "$pair_tool" prepare "$service" "$release/previous" "$service" "$deploy_root"
    systemctl daemon-reload
    echo "[PREPARE] $service pinned to its running image before the change: $release/previous"
  fi
  local captured_id endpoint
  captured_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["container_id"])' "$release/previous/pair.json")"
  [[ "$captured_id" =~ ^[0-9a-f]{64}$ ]] || die "$service has no captured container ID"
  if [[ "$mode" == quiesce ]]; then original_cids[$service]="$captured_id"; fi
  endpoint="$(postgres_endpoint_container "$captured_id")"
  [[ "$endpoint" == 'holo-postgres|5432' ]] || die "migration window requires a running direct TLS writer"
  postgres_endpoint_require_same "$endpoint" "$(postgres_endpoint_quadlet "$quadlet_dir/$service.container" "$compose_env")" ||
    die "$service installed endpoint differs from the captured running generation"
  printf '%s\n' "$endpoint" >"$release/previous.endpoint"
  python3 "$pair_tool" rollback "$service" "$release/previous" "$release/rollback.conf"
  (cd "$release" && sha256sum previous/pair.json previous.endpoint rollback.conf >rollback.sha256)
  python3 "$pair_tool" check "$service" "$release/previous"
  echo "[ROLLBACK] $release (schema/admission compatibility must be approved before a later manual apply)"
}
prepare_candidate() {
  stage=candidate-pair
  python3 "$pair_tool" candidate "$service" "$release/candidate" "${candidate_ids[$service]}" <"$quadlet_dir/$service.container"
  python3 "$pair_tool" check "$service" "$release/candidate"
  python3 "$pair_tool" verify-source "$service" "$release/candidate"
}

for service in "${services[@]}"; do
  echo "[INFO] service=$service revision=$revision candidate=${candidate_ids[$service]} migrator=$migrator_id"
  if [[ "$mode" == resume ]]; then
    release="${release_paths[$service]}"
    cmp -s "$release/migrator.image" <(printf '%s\n' "$migrator_id") || die "retained migrator identity mismatch"
    python3 "$pair_tool" verify "$service" "$release/candidate" "${candidate_ids[$service]}"
    python3 "$pair_tool" verify-source "$service" "$release/candidate"
  else
    prepare_slot
    prepare_candidate
  fi
  if [[ "$mode" == resume || "$mode" == quiesce ]]; then
    python3 "$pair_tool" verify-retained "$service" "$release/previous" "$release/candidate" "$deploy_root"
  fi
done

if [[ "$mode" == quiesce ]]; then
  stage=quiescence
  verify_original() {
    local name="$1" dir="${release_paths[$1]}"
    python3 "$pair_tool" verify-captured "$name" "$dir/previous" "$name"
    [[ "$(systemctl show "$name.service" -p ActiveState --value)" == active &&
      "$(systemctl show "$name.service" -p InvocationID --value)" == "$(<"$dir/captured.invocation")" ]] ||
      die "$name changed invocation after capture"
  }
  assert_infrastructure
  for service in "${services[@]}"; do verify_original "$service"; done
  quiescence_started=true
  ln -T -- "${release_paths[hololive-api]}/captured.invocation" "$owner_marker"
  sync_paths "$releases"
  for service in "${services[@]}"; do
    anchor="$(mask_anchor "$service")"
    ln -sT -- /dev/null "$anchor"
    stat -c '%d:%i' -- "$anchor" >"${release_paths[$service]}/mask.identity"
    mkdir -- "${release_paths[$service]}/stop"
    sync_paths "$mask_dir" "${release_paths[$service]}/mask.identity" "${release_paths[$service]}"
    guard arm "$service"
  done
  for service in "${services[@]}"; do
    guard check "$service"
    verify_original "$service"
  done
  for service in "${services[@]}"; do
    verify_original "$service"
    release="${release_paths[$service]}"
    stop_generation "$service" "${original_invocations[$service]}" "${original_cids[$service]}" "$release/stop" ||
      die "$service: original guarded stop unconfirmed; retain guard and evidence: $release/stop"
  done
  for service in "${services[@]}"; do
    guard seal "$service"
  done
  assert_coordinated_hold
  publish_receipt
  echo "[OUTCOME] quiesced api_release=${release_paths[hololive-api]} alarm_release=${release_paths[hololive-alarm-worker]}: migrate and activate only with explicit --resume after the coordinated peer cutover"
  exit 0
fi

stage=migration
run_migration() {
  local migrate_dropins="$quadlet_dir/hololive-db-migrate.container.d" migrate_tmp
  install -d -m 0755 "$migrate_dropins"
  migrate_tmp="$(mktemp "$migrate_dropins/.50-migrator-image.conf.XXXXXX")"
  printf '%s\n' \
    "# quadlet-redeploy-service.sh가 생성한 migrator image 고정 drop-in이다. 직접 고치지 않는다." \
    "[Container]" \
    "Image=$migrator_id" >"$migrate_tmp"
  chmod 0644 "$migrate_tmp"
  mv -f -- "$migrate_tmp" "$migrate_dropins/50-migrator-image.conf"
  systemctl daemon-reload
  assert_no_fence_marker
  for service in "${services[@]}"; do assert_writer_endpoint "$service"; done
  if [[ "$mode" == resume ]]; then
    check_receipt
    assert_infrastructure
    mkdir -- "$migration"
    sync_paths "$migration" "${release_paths[hololive-api]}"
  fi
  if [[ "$mode" == resume ]]; then
    python3 -I -B "$ROOT_DIR/scripts/deploy/lib/writer-drain-guard.py"
  else
    systemctl start hololive-db-migrate.service
  fi
  assert_no_fence_marker
  if [[ "$mode" == resume ]]; then
    printf '%s\n' "$migrator_id" >"$migration/completed"
    sync_paths "$migration/completed" "$migration"
  fi
  echo "[MIGRATE] hololive-db-migrate completed with $migrator_id"
}
if [[ "$mode" == resume ]]; then
  migration="${release_paths[hololive-api]}/migration"
  if [[ -e "$migration" ]]; then
    cmp -s "$migration/completed" <(printf '%s\n' "$migrator_id") ||
      die "previous migration failed or its outcome is unknown; not retrying"
  else
    run_migration
  fi
else
  run_migration
fi
require_card_ledger
stage=writer-sql
assert_infrastructure
for service in "${services[@]}"; do
  [[ "$mode" != resume ]] || assert_coordinated_hold
  assert_writer_endpoint "$service"
  postgres_writer_assert_sql "$service"
done
assert_infrastructure
assert_no_fence_marker

stage=activation
[[ "$mode" != resume ]] || check_receipt
if [[ "$mode" == resume ]]; then
  for service in "${services[@]}"; do
    release="${release_paths[$service]}"
    python3 "$pair_tool" verify-source "$service" "$release/candidate"
    python3 "$pair_tool" verify-retained "$service" "$release/previous" "$release/candidate" "$deploy_root"
  done
fi
activation_started=true
for service in "${services[@]}"; do
  release="${release_paths[$service]}"
  python3 "$pair_tool" activate "$service" "$release/candidate" "$deploy_root"
done
systemctl daemon-reload
for service in "${services[@]}"; do
  release="${release_paths[$service]}"
  stage=activation
  unit="$service.service"
  assert_infrastructure
  assert_writer_endpoint "$service"
  if [[ "$mode" == resume ]]; then
    if (( ${#opened[@]} == 0 )); then
      check_receipt
    fi
    if [[ "$service" == hololive-api ]]; then
      [[ "$(first_generation hololive-alarm-worker)" == "${opened_invocations[hololive-alarm-worker]} ${opened_cids[hololive-alarm-worker]}" ]] ||
        die "worker generation changed before API admission"
    fi
    assert_stopped "$service" || die "$unit: refusing to release a foreign or incomplete hold"
    if (( ${#opened[@]} == 0 )); then
      defer_signal=true
      (trap '' INT TERM HUP; exec mkdir -- "${release_paths[hololive-api]}/opening")
      opening_owned=true
      sync_paths "${release_paths[hololive-api]}"
      end_defer
    fi
    defer_signal=true
    opened+=("$service")
    (trap '' INT TERM HUP; exec systemctl unmask "$unit")
    unmasked[$service]=1
    end_defer
  fi
  restarts_before="$(systemctl show "$unit" -p NRestarts --value)"
  printf '%s\n' "$restarts_before" >"$release/nrestarts-before"
  if [[ "$mode" == resume ]]; then
    current_job="$(systemctl show "$unit" -p Job --value)"
    current_state="$(systemctl show "$unit" -p ActiveState --value)"
    if ! { [[ -z "$current_job" && "$current_state" == inactive ]] && container_absent "$service"; }; then
      die "$service: start 전 정지 상태 미확인"
    fi
    echo "[START] $unit (NRestarts before=$restarts_before)"
    defer_signal=true
    [[ -z "$pending_signal" ]] || exit "$pending_signal"
    since="@$EPOCHREALTIME"
    request="$(trap '' INT TERM HUP; request_start "$unit")" || die "$service: start job 미확인"
    read -r job client <<<"$request"
    if (( client != 0 )); then
      failed_start_jobs[$service]="$job"
      failed_start_times[$service]="$since"
      die "$service: start 클라이언트 실패 (job=$job, status=$client)"
    fi
    generation="$(trap '' INT TERM HUP; started_generation "$service" "$since" "$job")" ||
      die "$service: 최초 start job의 실행 세대 미확인"
    opened_invocations[$service]="${generation%% *}"
    opened_cids[$service]="${generation#* }"
    end_defer
  else
    echo "[RESTART] $unit (NRestarts before=$restarts_before)"
    systemctl restart "$unit"
  fi
  stage=verify
  systemctl is-active --quiet "$unit" || die "$unit is not active after restart"
  python3 "$pair_tool" verify-live "$service" "$release/candidate" "$service"
  restarts="$(systemctl show "$unit" -p NRestarts --value)"
  [[ "$restarts" == 0 ]] || die "$unit restarted automatically after the change (NRestarts=$restarts)"
done
if [[ "$mode" == resume ]]; then
  for service in "${services[@]}"; do
    [[ "$(first_generation "$service")" == "${opened_invocations[$service]} ${opened_cids[$service]}" ]] ||
      die "$service changed before release completion"
  done
  owns_hold || die "coordinated hold owner changed"
  assert_infrastructure
  defer_signal=true
  (trap '' INT TERM HUP; exec rm -- "$owner_marker")
  opening_owned=false
  sync_paths "$releases"
  end_defer
else
  assert_infrastructure
fi
for service in "${services[@]}"; do
  echo "[OUTCOME] deployed service=$service revision=$revision image=${candidate_ids[$service]} release=${release_paths[$service]}"
done
