#!/usr/bin/env bash
set -Eeuo pipefail
[[ $# == 7 || $# == 14 ]] || exit 2
slot="$1" mode="$2" staging="$3" backup="$4" revision="$5" version="$6"
seal="$7"
staged_pair=false card_transition=false backup_prefix="card-po-$slot"
if [[ "${backup##*/}" == staged-po-b-* ]]; then
  [[ "$slot" == b && ( "$mode" == quiesce || "$mode" == resume || "$mode" == rollback || "$mode" == check ) ]] || exit 2
  staged_pair=true backup_prefix=staged-po-b
  [[ $# == 14 ]] || exit 2
  window="$8" w2_revision="$9" w2_collector="${10}" w2_issuer="${11}" origin_digest="${12}" expires="${13}" tool_revision="${14}"
  [[ "$window" =~ ^[0-9a-f]{32}$ && "$w2_revision" =~ ^[0-9a-f]{40}$ &&
     "$w2_collector" =~ ^sha256:[0-9a-f]{64}$ && "$w2_issuer" =~ ^sha256:[0-9a-f]{64}$ &&
     "$origin_digest" =~ ^[0-9a-f]{64}$ && "$tool_revision" =~ ^[0-9a-f]{40}$ && "$expires" =~ ^[1-9][0-9]{0,11}$ ]] || exit 2
  if [[ "$mode" != check ]]; then (( EPOCHSECONDS < expires )) || exit 2; fi
else
  [[ $# == 7 ]] || exit 2
fi
case "$mode" in
  quiesce|resume|rollback|check)
    if [[ "$staged_pair" == true ]]; then [[ "$seal" =~ ^[0-9a-f]{64}$ ]] || exit 2
    else [[ "$seal" == - ]] || exit 2; fi ;;
  quiesce-verified) [[ "$seal" =~ ^[0-9a-f]{64}$ ]] || exit 2; card_transition=true mode=quiesce ;;
  *) [[ "$seal" == - ]] || exit 2 ;;
esac
[[ "${slot_slot:-}" == "$slot" && "${slot_deploy:-}" == pair && -n "${slot_root:-}" ]] || exit 2
slot_root="${slot_root:?collector slot preamble required}" slot_service="${slot_service:?collector slot preamble required}" slot_port="${slot_port:?collector slot preamble required}" slot_host="${slot_host:?collector slot preamble required}" slot_collector_unit="${slot_collector_unit:?collector slot preamble required}" slot_issuer_unit="${slot_issuer_unit:?collector slot preamble required}" slot_collector_image="${slot_collector_image:?collector slot preamble required}" slot_issuer_image="${slot_issuer_image:?collector slot preamble required}" slot_unit_dir="${slot_unit_dir:?collector slot preamble required}"
remote_root="$slot_root" service="$slot_service" port="$slot_port" host_label="$slot_host"
[[ "$mode" == deploy || "$mode" == quiesce || "$mode" == resume || "$mode" == rollback || "$mode" == check ]] || exit 2
[[ "$staging" == "$remote_root/po-staging-${backup##*-}" ]] || exit 2
[[ "$backup" =~ ^$remote_root/backups/$backup_prefix-[0-9]{8}T[0-9]{6}Z$ ]] || exit 2
[[ "$revision" =~ ^[0-9a-f]{40}$ && "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 2
install -d -m 0755 /run/lock
lock_name=hololive-quadlet-redeploy
[[ "$slot" != b ]] || lock_name="${slot_collector_unit%.service}-cutover"
exec 9>"/run/lock/$lock_name.lock"
flock -n 9 || { echo 'another Quadlet writer is running' >&2; exit 1; }
current="$remote_root/current"
unit_dir=/etc/containers/systemd/hololive-bot
manifest="../po-current-$slot/rootfs-manifest.json"
image_id_file="../po-current-$slot/image-id"
change_id="${backup##*-}"
profile_release="$current/artifacts/worker-profile/releases/${backup##*/}/$service"
collector="$slot_collector_image:prod"
issuer="$slot_issuer_image:prod"
collector_candidate="$slot_collector_image:candidate"
issuer_candidate="$slot_issuer_image:candidate"
collector_rollback="$slot_collector_image:rollback-$change_id"
issuer_rollback="$slot_issuer_image:rollback-$change_id"
collector_container="${slot_collector_unit%.service}"
issuer_container="${slot_issuer_unit%.service}"
collector_unit="$collector_container.service"
issuer_unit="$issuer_container.service"
hold="$remote_root/po-$slot.hold"
lock="$remote_root/po-$slot.lock"
card_active="$remote_root/po-$slot.card-active"
system_unit_dir=/etc/systemd/system
hold_owned=false
declare -A release_attempted=() launched_inv=() launched_gen=() failed_start_job=() failed_start_since=()
[[ "$(stat -c '%u:%g %a' "$staging")" == '0:0 700' ]] || { echo "$host_label staged artifacts are not root-private" >&2; exit 1; }
cd "$current"
if [[ "$slot" == b && "$staged_pair" == false && -e "$staging/transport-files.sha256" &&
      ( "$mode" == deploy || ( "$mode" == quiesce && -f "$card_active" ) ) ]]; then
  echo 'sealed b staging with an admitted publisher requires the staged quiesce grant' >&2; exit 1
fi
if [[ "$staged_pair" == true ]]; then
  [[ "$(realpath -e -- "$staging")" == "$staging" && "$(realpath -e -- "${backup%/*}")" == "${backup%/*}" ]]
  unsafe="$(find "$staging" \( \( ! -type f ! -type d \) -o ! -uid 0 -o ! -gid 0 -o -perm /022 \) -print -quit)"
  [[ -z "$unsafe" ]] || { echo 'untrusted staged file type, ownership or permissions' >&2; exit 1; }
  (
    cd -- "$staging"
    sha256sum source-files.sha256 rootfs-manifest.json collector-manifest.json image-id collector-image-id \
      image.tar.sha256 collector-image.tar.sha256 | cmp -s -- - transport-files.sha256
    for archive in image.tar collector-image.tar; do
      read -r digest name extra < "$archive.sha256"
      [[ "$digest" =~ ^[0-9a-f]{64}$ && "$name" == "$archive" && -z "$extra" ]]
      cmp -s -- "$archive.sha256" <(printf '%s  %s\n' "$digest" "$archive")
    done
  )
  if [[ "$mode" == quiesce ]]; then
    [[ ! -e "$backup" && ! -L "$backup" ]] || exit 1
  else
    [[ ! -L "$backup" && "$(stat -c '%u:%g %a' "$backup")" == '0:0 700' ]] || exit 1
  fi
fi
if [[ "$mode" == resume || "$mode" == rollback || "$mode" == check ]]; then
  if [[ "$mode" == check ]]; then cmp -s -- "$backup/card-candidate" "$card_active" || exit 1; fi
  if [[ "$staged_pair" == true ]]; then
    [[ -f "$backup/card-origin" && -f "$backup/transport.sha256" ]] || exit 1
    if [[ "$mode" != check ]]; then cmp -s -- "$backup/owner-boot" /proc/sys/kernel/random/boot_id || exit 1; fi
    cmp -s -- "$backup/owner-tool" <(printf '%s\n' "$tool_revision") || exit 1
    cmp -s -- "$backup/candidate.revision" <(printf '%s\n' "$revision") || exit 1
    cmp -s -- "$backup/candidate.version" <(printf '%s\n' "$version") || exit 1
    cmp -s -- "$backup/transport.sha256" <(printf '%s\n' "$seal") || exit 1
    printf '%s  %s\n' "$origin_digest" "$backup/owner-origin" | sha256sum --check --strict --status -
    cmp -s -- "$backup/card-origin" <(head -n 1 -- "$backup/owner-origin")
    if [[ "$mode" != rollback ]]; then
      cmp -s -- "$backup/owner-window" <(printf '%s %s\n' "$window" "$expires") || exit 1
    fi
  elif [[ -f "$backup/transport.sha256" ]]; then
    card_transition=true
    [[ "$mode" != rollback ]] || { echo 'card transition cannot restore the pre-card publisher' >&2; exit 1; }
  fi
  if [[ -f "$backup/transport.sha256" ]]; then
    seal="$(<"$backup/transport.sha256")"
    [[ "$seal" =~ ^[0-9a-f]{64}$ ]] || exit 1
  fi
  if [[ "$card_transition" == false && "$mode" == resume ]]; then
    cmp -s -- "$backup/card-origin" "$card_active" || exit 1
  fi
  (cd "$staging" && sha256sum --check --strict "$backup/candidate-inputs.sha256")
fi
if [[ "$seal" != - ]]; then
  (cd "$staging" && printf '%s  transport-files.sha256\n' "$seal" | sha256sum --check --strict --status - &&
    sha256sum --check --strict transport-files.sha256)
fi
if [[ "$card_transition" == false && ( "$mode" == deploy || "$mode" == quiesce ) ]]; then
  [[ -f "$card_active" ]] || { echo 'first card cutover requires quiesce-verified and a new hold' >&2; exit 1; }
fi
if [[ "$mode" == quiesce || "$mode" == deploy || ( "$staged_pair" == true && "$mode" != resume ) ]]; then
  (cd "$staging" && sha256sum --check --strict image.tar.sha256 collector-image.tar.sha256)
fi
(cd "$staging" && sha256sum --check --strict source-files.sha256)
. "$staging/scripts/deploy/lib/collector-slots.sh"
collector_slot_matches "$slot" "$staging/scripts/deploy/lib/collector-slots.tsv" ||
  { echo "$host_label staged collector slot table differs from the caller's slot values" >&2; exit 1; }
. "$staging/scripts/deploy/lib/po-sandbox-image.sh"
. "$staging/scripts/deploy/lib/compose-env.sh"
. "$staging/scripts/deploy/lib/postgres-endpoint.sh"
. "$staging/scripts/deploy/lib/postgres-capacity.sh"
. "$staging/scripts/deploy/lib/ap-collector-readiness.sh"
compose_env=/etc/stack-secrets/hololive-bot/compose.env
pair_tool="$staging/scripts/deploy/lib/worker-profile-pair.py"
cutover_started=false

claim_slot() {
  [[ ! -L "$lock" ]] || return 1
  (umask 077; : >> "$lock")
  [[ "$(stat -c '%u:%g %a' "$lock")" == '0:0 600' ]] || return 1
  exec {slot_lock_fd}>>"$lock"
  flock -n "$slot_lock_fd" || { echo "$host_label paired operation already running" >&2; return 1; }
  if [[ -e "$hold" || -L "$hold" ]]; then
    [[ ! -L "$hold" && "$(stat -c '%u:%g %a' "$hold")" == '0:0 700' &&
       -f "$hold/backup" && ! -L "$hold/backup" && "$(<"$hold/backup")" == "$backup" ]] || {
      echo "$host_label foreign maintenance hold; retain $hold" >&2; return 1;
    }
    [[ "$mode" == resume || "$mode" == rollback ]] || {
      echo "$host_label maintenance held; use resume or approved rollback $backup" >&2; return 1;
    }
    hold_owned=true
  else
    [[ "$mode" != resume && ( "$staged_pair" != true || "$mode" != rollback ) ]] || {
      echo "$host_label $mode requires an owned hold" >&2; return 1;
    }
  fi
}

require_unmasked_units() {
  local unit state
  for unit in "$issuer_unit" "$collector_unit"; do
    [[ ! -e "$system_unit_dir/$unit" && ! -L "$system_unit_dir/$unit" ]] || return 1
    state="$(systemctl show -p LoadState --value "$unit")" || return "$?"
    [[ "$state" == loaded ]] || { echo "$unit is not an unmasked loaded unit" >&2; return 1; }
  done
}

require_owned_hold() {
  [[ "$hold_owned" == true && "$(<"$hold/backup")" == "$backup" ]] || return 1
  [[ -r "$backup/snapshot-complete" && "$(<"$backup/candidate.revision")" == "$revision" &&
     "$(<"$backup/candidate.version")" == "$version" ]] || return 1
  local inode
  inode="$(stat -c '%d:%i' "$backup/card-candidate")" || return 1
  [[ "$(<"$hold/card-candidate")" == "$inode" ]] || return 1
}

active_invocation() {
  local id
  systemctl is-active --quiet "$1" || return 1
  id="$(systemctl show -p InvocationID --value "$1")" || return 1
  [[ "$id" =~ ^[0-9a-f]{32}$ && "$id" != 00000000000000000000000000000000 ]] || return 1
  printf '%s\n' "$id"
}

container_generation() {
  local generation
  generation="$(podman inspect -f '{{.Id}} {{.Image}} {{.State.StartedAt.UnixNano}} {{.State.Pid}} {{.State.ConmonPid}}' "$1")" || return 1
  [[ "$generation" =~ ^[0-9a-f]{64}\ [0-9a-f]{64}\ [1-9][0-9]*\ [1-9][0-9]*\ [1-9][0-9]{0,9}$ &&
    "$(systemctl show -p MainPID --value "$1.service")" == "${generation##* }" ]] || return 1
  printf '%s\n' "$generation"
}

require_writer_window() {
  if [[ "$staged_pair" == true ]] && (( EPOCHSECONDS >= expires )); then
    echo "owner window expired; no new writer action submitted" >&2
    return 1
  fi
}

writer_change() {
  require_writer_window || return 1
  "$@"
}

stop_manager_invocation() {
  local LC_ALL=C since="$3" record journal_status=0
  writer_change systemctl stop "$1" || return 1
  record="$(journalctl --boot --quiet --no-pager -n 1 -o json --output-fields=INVOCATION_ID --since="$since" \
    _PID=1 "UNIT=$1" "INVOCATION_ID=$2" JOB_TYPE=stop MESSAGE_ID=de5b426a63be47a7b6ac3eaac82e2f6f)" || journal_status=$?
  if (( journal_status != 0 )) || [[ -z "$record" ]] ||
     ! python3 -c 'import json,sys; sys.exit(json.load(sys.stdin).get("INVOCATION_ID") != sys.argv[1])' "$2" <<< "$record"; then
    printf '[hold] stop journal unconfirmed: unit=%s invocation=%s since=%s journal_status=%d\n' \
      "$1" "$2" "$since" "$journal_status" >&2
    return 1
  fi
}

mask_anchor() {
  printf '%s/.po-%s-%s-%s.mask\n' "$system_unit_dir" "$slot" "$change_id" "$1"
}

require_mask_inode() {
  local unit="$1" path="$2" inode
  [[ -s "$hold/$unit" && ! -L "$hold/$unit" && "$(stat -c '%u:%g' "$path")" == 0:0 &&
     "$(readlink "$path")" == /dev/null ]] || return 1
  inode="$(stat -c '%d:%i' "$path")" || return 1
  [[ "$inode" == "$(<"$hold/$unit")" ]]
}

stop_guard() {
  local op="$1" unit="$2" anchor
  anchor="$(mask_anchor "$unit")" || return "$?"
  require_mask_inode "$unit" "$anchor" || return 1
  local -a command=(python3 -I -B "$staging/scripts/deploy/lib/systemd-stop-guard.py" "$op" "$unit" "$anchor" "$(<"$hold/$unit")")
  case "$op" in
    arm|seal|undo-unmask) writer_change "${command[@]}" ;;
    check|start-budget) "${command[@]}" ;;
    *) return 1 ;;
  esac
}

container_absent() {
  local exists=0
  podman container exists "$1" || exists=$?
  [[ "$exists" == 1 ]] || { echo "container absence unconfirmed for $1 (status=$exists)" >&2; return 1; }
}

require_stopped_unit() {
  local unit="$1" state invocation job cid
  [[ -f "$hold/$unit.stop-cid" && ! -L "$hold/$unit.stop-cid" ]] || return 1
  cid="$(<"$hold/$unit.stop-cid")"
  [[ "$cid" =~ ^[0-9a-f]{64}$ ]] || return 1
  job="$(systemctl show -p Job --value "$unit")" || return 1
  [[ -z "$job" && "$(systemctl show -p LoadState --value "$unit")" == masked &&
     "$(systemctl show -p MainPID --value "$unit")" == 0 &&
     "$(systemctl show -p ControlPID --value "$unit")" == 0 ]] || return 1
  state="$(systemctl show -p ActiveState --value "$unit")" || return "$?"
  if [[ "$state" != inactive ]]; then
    [[ "$unit" == "$issuer_unit" && "$state" == failed && -s "$hold/issuer-stop.invocation" ]] || return 1
    invocation="$(<"$hold/issuer-stop.invocation")"
    [[ "$invocation" =~ ^[0-9a-f]{32}$ &&
       "$(systemctl show -p InvocationID --value "$unit")" == "$invocation" &&
       "$(systemctl show -p Result --value "$unit")" == exit-code &&
       "$(systemctl show -p ExecMainCode --value "$unit")" == 1 &&
       "$(systemctl show -p ExecMainStatus --value "$unit")" == 143 ]] || return 1
  fi
  container_absent "$cid" && container_absent "${unit%.service}"
}

require_held_unit() {
  local unit="$1"
  require_mask_inode "$unit" "$(mask_anchor "$unit")" &&
    require_mask_inode "$unit" "$system_unit_dir/$unit" && require_stopped_unit "$unit"
}

require_held_units() {
  require_owned_hold && require_held_unit "$issuer_unit" && require_held_unit "$collector_unit"
}

require_confirmed_hold() {
  [[ -f "$hold/quiesced" && ! -L "$hold/quiesced" && "$(<"$hold/quiesced")" == guarded-stop-v1 ]] || {
    echo 'guarded quiesce was not confirmed; retain the maintenance hold' >&2; return 1;
  }
  require_held_units
}

record_confirmed_hold() {
  if ! { require_held_units &&
    sync -- "$hold/issuer-stop.invocation" "$hold/$issuer_unit.stop-cid" "$hold/$collector_unit.stop-cid" &&
    printf 'guarded-stop-v1\n' > "$hold/quiesced" && sync -- "$hold/quiesced" "$hold"; }; then
    rm -f -- "$hold/quiesced"; return 1
  fi
}

create_hold() {
  local unit anchor
  mkdir -m 0700 -- "$hold" || return "$?"
  printf '%s\n' "$backup" > "$hold/backup" || return "$?"
  stat -c '%d:%i' "$backup/card-candidate" > "$hold/card-candidate" || return "$?"
  hold_owned=true
  for unit in "$issuer_unit" "$collector_unit"; do
    anchor="$(mask_anchor "$unit")" || return "$?"
    writer_change ln -sT -- /dev/null "$anchor" || return "$?"
    stat -c '%d:%i' "$anchor" > "$hold/$unit" || return "$?"
  done
  sync -- "$backup/card-candidate" "$backup" "$hold/card-candidate" "$hold/backup" \
    "$hold/$issuer_unit" "$hold/$collector_unit" "$system_unit_dir" "$hold" "$remote_root"
}

unit_status() {
  systemctl show --all -p ActiveState -p InvocationID -p Result -p ExecMainCode -p ExecMainStatus \
    -p MainPID -p ControlPID "$1"
}

record_stop_evidence() {
  printf '%s unit=%s %s\n%s\n' "$EPOCHREALTIME" "$1" "$2" "$3" >> "$backup/guarded-stop.log" &&
    sync -- "$backup/guarded-stop.log" "$backup"
}

require_stop_generation() {
  local unit="$1" invocation="$2" generation="$3" phase="$4"
  if [[ "$phase" == initial ]]; then
    if [[ "$unit" == "$issuer_unit" ]]; then
      verify_issuer_runtime "$old_po_revision" "$issuer_image_id" || return 1
    else
      python3 "$pair_tool" verify-captured "$service" "$profile_release/previous" "$collector_container" || return 1
    fi
  elif [[ "$phase" == rehold ]]; then
    [[ "$(systemctl show -p NRestarts --value "$unit")" == 0 ]] || return 1
  fi
  [[ -n "$generation" && -n "$invocation" && "$(container_generation "${unit%.service}")" == "$generation" &&
     "$(active_invocation "$unit")" == "$invocation" ]]
}

stop_guarded_unit() {
  local unit="$1" invocation="$2" generation="$3" phase="$4" managed=false accepted=false snapshot key value
  local LC_ALL=C since='' exit_status=unknown
  local -A observed=()
  if [[ -n "$generation" && -n "$invocation" ]]; then
    require_stop_generation "$unit" "$invocation" "$generation" "$phase" || return 1
    stop_guard check "$unit" || return 1
    since="@$EPOCHREALTIME"
    if stop_manager_invocation "$unit" "$invocation" "$since"; then managed=true; fi
  fi
  snapshot="$(unit_status "$unit")" || return 1
  if [[ -n "$since" ]]; then
    exit_status="$(python3 -I -B "$staging/scripts/deploy/lib/systemd-stop-guard.py" \
      exit-status "$unit" "${generation%% *}" "$since")" || exit_status=unknown
  fi
  record_stop_evidence "$unit" "stop=$managed invocation=$invocation since=$since exit_status=$exit_status cid=${generation%% *}" "$snapshot" || return 1
  while IFS='=' read -r key value; do
    [[ -n "$key" ]] || return 1
    observed[$key]="$value"
  done <<< "$snapshot"
  [[ "${observed[MainPID]-}" == 0 && "${observed[ControlPID]-}" == 0 &&
     ( "${observed[ActiveState]-}" == inactive || "${observed[ActiveState]-}" == failed ) ]] || return 1
  container_absent "${unit%.service}" || return 1
  if [[ -n "$generation" ]]; then container_absent "${generation%% *}" || return 1; fi
  if [[ "$managed" == true ]]; then
    if [[ "$exit_status" == 0 && "${observed[ActiveState]}" == inactive && "${observed[Result]-}" == success &&
          -v 'observed[InvocationID]' && ( -z "${observed[InvocationID]}" || "${observed[InvocationID]}" == "$invocation" ) &&
          ( "${observed[ExecMainCode]-}" == 1 || "${observed[ExecMainCode]-}" == 0 ) && "${observed[ExecMainStatus]-}" == 0 ]]; then
      accepted=true
    elif [[ "$unit" == "$issuer_unit" && "$exit_status" == 143 && "${observed[ActiveState]}" == failed &&
            "${observed[InvocationID]-}" == "$invocation" && "${observed[ExecMainCode]-}" == 1 &&
            "${observed[Result]-}" == exit-code && "${observed[ExecMainStatus]-}" == 143 ]]; then
      accepted=true
    fi
  fi
  if [[ "$accepted" != true ]]; then
    record_stop_evidence "$unit" 'unconfirmed' "$snapshot" || return 1
    return 2
  elif [[ "$unit" == "$issuer_unit" ]]; then
    printf '%s\n' "$invocation" > "$hold/issuer-stop.invocation" || return 1
  fi
  printf '%s\n' "${generation%% *}" > "$hold/$unit.stop-cid"
}

hold_units() {
  local phase="$1" unit status=0 snapshot outcome all_observed=true identity
  local -a opened=() armed=() sealable=()
  local -A inv=() gen=()
  require_owned_hold || return 1
  if [[ "$phase" == rehold ]]; then
    rm -f -- "$hold/quiesced" && sync -- "$hold" || status=1
  fi
  for unit in "$issuer_unit" "$collector_unit"; do
    if [[ "$phase" == rehold ]]; then
      if [[ ! -v 'release_attempted[$unit]' || -L "$system_unit_dir/$unit" || -e "$system_unit_dir/$unit" ]]; then
        require_held_unit "$unit" || status=1
        [[ "${release_attempted[$unit]-}" != opened ]] || status=1
        continue
      fi
      if [[ "${release_attempted[$unit]}" == opening ]] &&
         require_mask_inode "$unit" "$(mask_anchor "$unit")" && require_stopped_unit "$unit"; then
        if stop_guard undo-unmask "$unit" && require_held_unit "$unit"; then continue; fi
        status=1
      fi
    fi
    opened+=("$unit")
    if [[ "$phase" == initial ]]; then
      if [[ "$unit" == "$issuer_unit" ]]; then inv[$unit]="$issuer_invocation"; gen[$unit]="$captured_issuer"
      else inv[$unit]="$collector_invocation"; gen[$unit]="$captured_generation"; fi
    else
      snapshot="$(unit_status "$unit")" || { snapshot=unknown; status=1; }
      record_stop_evidence "$unit" pre-arm "$snapshot" || status=1
      if [[ "$phase" == rehold ]]; then
        inv[$unit]="${launched_inv[$unit]-}" gen[$unit]="${launched_gen[$unit]-}"
      else
        inv[$unit]="$(active_invocation "$unit")" || inv[$unit]=''
        gen[$unit]="$(container_generation "${unit%.service}")" || gen[$unit]=''
      fi
    fi
  done
  for unit in "${opened[@]}"; do
    if stop_guard arm "$unit"; then armed+=("$unit"); else status=1; fi
  done
  if [[ "$phase" == initial ]]; then
    (( status == 0 )) || return 1
    for unit in "${armed[@]}"; do
      stop_guard check "$unit" && require_stop_generation "$unit" "${inv[$unit]}" "${gen[$unit]}" "$phase" || return 1
    done
  fi
  for unit in "${armed[@]}"; do
    if [[ "$phase" == rehold && -v 'failed_start_job[$unit]' ]]; then status=1; fi
    if [[ "$phase" == rehold && ! -v 'launched_gen[$unit]' ]]; then
      status=1
      if [[ "$staged_pair" == true || ! -v 'failed_start_job[$unit]' ]]; then continue; fi
      local budget
      budget="$(stop_guard start-budget "$unit")" && [[ "$budget" =~ ^[1-9][0-9]*$ ]] || continue
      if ! identity="$(started_slot_generation "$unit" "${failed_start_since[$unit]}" "${failed_start_job[$unit]}" "$((SECONDS + budget + 1))")"; then continue; fi
      inv[$unit]="${identity%% *}"
      gen[$unit]="${identity#* }"
    fi
    outcome=0
    stop_guarded_unit "$unit" "${inv[$unit]}" "${gen[$unit]}" "$phase" || outcome=$?
    case "$outcome" in
      0) sealable+=("$unit") ;;
      2) sealable+=("$unit"); status=1 ;;
      *) status=1
         snapshot="$(unit_status "$unit")" && record_stop_evidence "$unit" unconfirmed "$snapshot" || all_observed=false ;;
    esac
  done
  [[ "$all_observed" == true ]] || return 1
  for unit in "${sealable[@]}"; do stop_guard seal "$unit" || status=1; done
  (( status == 0 )) && record_confirmed_hold
}

release_unit() {
  local unit="$1"
  require_owned_hold || return 1
  require_held_unit "$unit" || return 1
  require_writer_window || return 1
  if (( ${#release_attempted[@]} == 0 )); then
    release_attempted[$unit]=invalidating
    rm -- "$hold/quiesced" && sync -- "$hold" || return 1
  fi
  release_attempted[$unit]=opening
  writer_change systemctl unmask "$unit" || return "$?"
  release_attempted[$unit]=opened
}

request_slot_start() {
  local LC_ALL=C unit="$1" since transaction line job='' client=0
  since="@$EPOCHREALTIME"
  transaction="$(LC_ALL=C SYSTEMD_LOG_COLOR=0 SYSTEMD_LOG_LEVEL=info SYSTEMD_LOG_TARGET=console \
    writer_change systemctl --show-transaction start "$unit" 2>&1)" || client=$?
  while IFS= read -r line; do
    if [[ "$line" =~ ^Enqueued\ anchor\ job\ ([1-9][0-9]*)\ (.*)/start\.$ && "${BASH_REMATCH[2]}" == "$unit" ]]; then
      [[ -z "$job" ]] || return 1
      job="${BASH_REMATCH[1]}"
    fi
  done <<< "$transaction"
  [[ -n "$job" ]] || { echo "[start] $unit: original manager job not identified (client=$client)" >&2; return 1; }
  printf '%s %s %s\n' "$since" "$job" "$client"
}

systemd_unit_object() {
  local object="${1//-/_2d}"
  printf '/org/freedesktop/systemd1/unit/%s\n' "${object//./_2e}"
}

manager_start_budget() {
  local bus=org.freedesktop.systemd1 object
  object="$(systemd_unit_object "$1")" || return 1
  busctl --system --timeout=45s --json=short get-property "$bus" "$object" \
    "$bus.Service" TimeoutStartUSec | python3 -c '
import json, sys
p = json.load(sys.stdin)
n = p["data"]
if p["type"] != "t" or type(n) is not int or not 0 < n < (1 << 64) - 1:
    sys.exit("finite manager start timeout required")
print((n + 999999) // 1000000)'
}

started_slot_generation() {
  local unit="$1" since="$2" job="$3" limit="${4:-0}" current record invocation generation
  if (( limit != 0 )); then
    while :; do
      current="$(systemctl show -p Job --value "$unit")" || return 1
      [[ -n "$current" ]] || break
      [[ "$current" == "$job" ]] && (( SECONDS < limit )) || return 1
      sleep 0.25 || return 1
    done
  fi
  record="$(journalctl --boot --quiet --no-pager -o json --output-fields=INVOCATION_ID --since="$since" \
    _PID=1 "UNIT=$unit" "JOB_ID=$job" JOB_TYPE=start JOB_RESULT=done MESSAGE_ID=39f53479d3a045ac8e11786248231fbf)" || return 1
  invocation="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["INVOCATION_ID"])' <<< "$record")" || return 1
  [[ "$invocation" =~ ^[0-9a-f]{32}$ && "$(active_invocation "$unit")" == "$invocation" &&
     "$(systemctl show -p NRestarts --value "$unit")" == 0 ]] || return 1
  generation="$(container_generation "${unit%.service}")" || return 1
  if [[ "$staged_pair" == true ]]; then
    local kind=collector expected="$staging/collector-image-id" old_inv old_cid cid raw_image rest
    if [[ "$unit" == "$issuer_unit" ]]; then kind=issuer expected="$staging/image-id"; fi
    if [[ "$mode" == rollback ]]; then
      if [[ "$kind" == issuer ]]; then expected="$backup/po-prechange.image-id"; else expected="$backup/collector-prechange.image-id"; fi
    fi
    read -r old_inv old_cid < <(awk -v kind="$kind" '$1 == kind {print $2, $3}' "$backup/owner-origin") || return 1
    read -r cid raw_image rest <<<"$generation"
    [[ "$old_inv" != "$invocation" && "$old_cid" != "$cid" && -n "$rest" && "sha256:$raw_image" == "$(<"$expected")" ]] || return 1
  fi
  require_stop_generation "$unit" "$invocation" "$generation" rehold || return 1
  printf '%s %s\n' "$invocation" "$generation"
}

start_slot_unit() {
  local unit="$1" defer_signal=true pending_signal=0 status=0 request since job identity budget deadline=0 wait_until=0 receipt_status=0
  if [[ "$hold_owned" != true ]]; then writer_change systemctl restart "$unit" || return $?; return 0; fi
  if [[ "$staged_pair" == true ]]; then
    budget="$(manager_start_budget "$unit")" && [[ "$budget" =~ ^[1-9][0-9]*$ ]] || return 1
    deadline=$((SECONDS + budget + 1))
  fi
  if (( pending_signal == 0 )); then
    if request="$(trap '' INT TERM HUP; request_slot_start "$unit")"; then
      read -r since job status <<<"$request"
      if [[ "$staged_pair" == true ]]; then
        (set -C; printf '%s %s %s\n' "$since" "$job" "$status" > "$backup/$mode-$unit.start-request") &&
          sync -- "$backup/$mode-$unit.start-request" "$backup" || receipt_status=1
      fi
      if (( status != 0 )); then
        failed_start_job[$unit]="$job" failed_start_since[$unit]="$since"
        wait_until="$deadline"
      fi
      if (( status == 0 )) || [[ "$staged_pair" == true ]]; then
        if identity="$(trap '' INT TERM HUP; started_slot_generation "$unit" "$since" "$job" "$wait_until")"; then
          launched_inv[$unit]="${identity%% *}" launched_gen[$unit]="${identity#* }"
          if [[ "$staged_pair" == true ]]; then
            (set -C; printf '%s %s %s\n' "$since" "$job" "$identity" > "$backup/$mode-$unit.start") &&
              sync -- "$backup/$mode-$unit.start" "$backup" || receipt_status=1
          fi
        elif (( status == 0 )); then
          status=1
        fi
      fi
    else
      status=$?
    fi
  fi
  defer_signal=false
  if (( pending_signal != 0 )); then maintenance_failed "$pending_signal"; fi
  if (( status == 0 && receipt_status != 0 )); then return "$receipt_status"; fi
  return "$status"
}

require_launched_unit() {
  require_stop_generation "$1" "${launched_inv[$1]-}" "${launched_gen[$1]-}" rehold
}

clear_hold() {
  local unit anchor retired hold="$hold"
  require_owned_hold && require_unmasked_units || return 1
  for unit in "$issuer_unit" "$collector_unit"; do
    anchor="$(mask_anchor "$unit")"
    require_mask_inode "$unit" "$anchor" || return 1
    if [[ -v 'launched_inv[$unit]' ]]; then require_launched_unit "$unit" || return 1; fi
  done
  retired="$(mktemp -d -- "$remote_root/.po-$slot-released.XXXXXX")" || return "$?"
  trap '' INT TERM HUP
  writer_change mv -T -- "$hold" "$retired/hold" || return "$?"
  hold="$retired/hold"
  hold_owned=false
  trap 'echo "activation verified; retired hold cleanup incomplete" >&2' ERR
  sync -- "$remote_root" "$retired"
  for unit in "$issuer_unit" "$collector_unit"; do
    anchor="$(mask_anchor "$unit")"
    require_mask_inode "$unit" "$anchor" || return 1
    writer_change rm -- "$anchor" || return 1
  done
  rm -- "$hold/$issuer_unit" "$hold/$collector_unit" "$hold/backup" "$hold/card-candidate"
  rm -f -- "$hold/issuer-stop.invocation" "$hold/$issuer_unit.stop-cid" "$hold/$collector_unit.stop-cid" "$hold/quiesced"
  rmdir -- "$hold" "$retired"
}

maintenance_failed() {
  local status="${1:-1}"
  [[ "$BASHPID" == "$maintenance_owner_pid" ]] || exit "$status"
  trap '' INT TERM HUP
  trap - ERR
  set +e
  if { (( ${#release_attempted[@]} != 0 )) && hold_units rehold; } ||
     { (( ${#release_attempted[@]} == 0 )) && require_confirmed_hold; }; then
    echo "[OUTCOME] held: both writers inactive; retain $backup and $hold" >&2
  else
    echo "[OUTCOME] unconfirmed: maintenance hold incomplete; do not mutate DB; retain $backup and $hold" >&2
  fi
  exit "$status"
}

maintenance_signal() {
  if [[ "${defer_signal:-false}" == true ]]; then
    if (( pending_signal == 0 )); then pending_signal="$1"; fi
  else
    maintenance_failed "$1"
  fi
}

arm_maintenance_failure() {
  maintenance_owner_pid="$BASHPID"
  trap 'maintenance_failed $?' ERR
  trap 'maintenance_signal 130' INT
  trap 'maintenance_signal 143' TERM
  trap 'maintenance_signal 129' HUP
}

verify_candidate_images() {
  po_verify_image "$issuer_candidate" "$staging/rootfs-manifest.json" "$revision" arm64 "$staging/image-id"
  verify_collector "$collector_candidate" "$staging/collector-manifest.json" "$revision" "$staging/collector-image-id"
  [[ "$(image_label "$issuer_candidate" version)" == "$version" ]]
  [[ "$(image_label "$collector_candidate" version)" == "$version" ]]
}

prepare_candidate_pair() {
  local candidate_image env_file=- expected previous_revision
  candidate_image="$(image_id "$collector_candidate")"
  if [[ "$mode" == resume ]]; then
    if [[ "$slot" == c ]]; then
      [[ "$(<"$current/.source-revision")" == "$revision" ]] || return 1
    else
      cmp -- "$staging/$slot_unit_dir/$collector_container.container" \
        "$unit_dir/$collector_container.container" || return "$?"
    fi
    [[ "$slot" != c ]] || env_file="${compose_env:-/etc/stack-secrets/hololive-bot/compose.env}"
    if [[ "$slot" == c ]]; then expected='holo-postgres|5432'; else expected='100.100.1.8|5433'; fi
    postgres_endpoint_require_same "$(postgres_endpoint_quadlet "$unit_dir/$collector_container.container" "$env_file")" "$expected"
    previous_revision="$(cat "$backup/collector.revision")"
    verify_collector "$collector_rollback" "$backup/collector-prechange-manifest.json" "$previous_revision" "$backup/collector-prechange.image-id"
    [[ "$(<"$backup/issuer.state")" == present ]]
    po_verify_image "$issuer_rollback" "$backup/po-prechange-manifest.json" "$previous_revision" arm64 "$backup/po-prechange.image-id"
    python3 "$pair_tool" verify "$service" "$profile_release/previous" "$collector_rollback"
    require_card_candidate || return 1
  fi
  if [[ ! -e "$profile_release/candidate" ]]; then
    [[ "$staged_pair" != true || "$mode" != resume ]] || { echo 'preflighted candidate profile is missing' >&2; return 1; }
    writer_change python3 "$pair_tool" candidate "$service" "$profile_release/candidate" "$candidate_image" \
      < "$unit_dir/$collector_container.container"
  fi
  python3 "$pair_tool" verify "$service" "$profile_release/candidate" "$candidate_image"
  python3 "$pair_tool" verify-dropin "$service" "$profile_release/candidate"
  cmp -- "$profile_release/candidate/unit.container" "$unit_dir/$collector_container.container"
  python3 "$pair_tool" check "$service" "$profile_release/candidate"
  python3 "$pair_tool" verify-source "$service" "$profile_release/candidate"
}

validate_candidate_pair() {
  prepare_candidate_pair
  python3 "$pair_tool" check-database "$service" "$profile_release/candidate"
}

image_id() {
  local id
  id="$(podman image inspect -f '{{.Id}}' "$1")" || return "$?"
  printf 'sha256:%s\n' "$id"
}
container_image_id() {
  local id
  id="$(podman inspect -f '{{.Image}}' "$1")" || return "$?"
  printf 'sha256:%s\n' "$id"
}
image_label() {
  podman image inspect -f "{{index .Config.Labels \"org.opencontainers.image.$2\"}}" "$1"
}
issuer_presence() {
  local status=0
  podman image exists "$issuer" || status="$?"
  case "$status" in
    0) printf 'present\n' ;;
    1) printf 'absent\n' ;;
    *) echo "$host_label issuer image inventory failed (status=$status)" >&2; return 1 ;;
  esac
}
verify_collector() {
  local image="$1" reviewed="$2" expected="$3" id_file="$4" actual_id actual_arch actual_revision
  [[ "$expected" =~ ^[0-9a-f]{40}$ ]] || return 1
  actual_id="$(image_id "$image")" || return 1
  po_image_id_matches "$actual_id" "$(cat "$id_file")" || return 1
  python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); assert m["source_revision"] == sys.argv[2] and m["go"]["goarch"] == "arm64" and m.get("image_id",sys.argv[3]) == sys.argv[3]' "$reviewed" "$expected" "$actual_id"
  actual_arch="$(podman image inspect -f '{{.Os}}/{{.Architecture}}' "$image")"
  actual_revision="$(image_label "$image" revision)"
  [[ "$actual_arch" == linux/arm64 && "$actual_revision" == "$expected" ]]
}
verify_issuer_runtime() {
  local started="$1" reviewed="$2" socket_mount running expected
  systemctl is-active --quiet "$issuer_unit" || return 1
  [[ "$(podman inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$issuer_container")" == healthy ]] || return 1
  [[ "$(podman inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$issuer_container")" == "$started" ]] || return 1
  running="$(container_image_id "$issuer_container")" || return 1
  expected="$(image_id "$issuer")" || return 1
  po_image_id_matches "$running" "$reviewed" && [[ "$running" == "$expected" ]] || return 1
  socket_mount="$(podman inspect -f '{{range .Mounts}}{{if eq .Destination "/run/hololive-youtube-po"}}{{.Source}}{{end}}{{end}}' "$issuer_container")" || return 1
  [[ "$(stat -c '%u:%g %a' "$socket_mount")" == '65532:1000 770' ]] || return 1
  podman exec "$issuer_container" /app/bin/po-broker --healthcheck --socket /run/hololive-youtube-po/worker.sock
}
verify_collector_runtime() {
  local expected="$1" reviewed="$2" running tagged ready
  systemctl is-active --quiet "$collector_unit" || return 1
  [[ "$(podman inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$collector_container")" == healthy ]] || return 1
  [[ "$(podman inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$collector_container")" == "$expected" ]] || return 1
  running="$(container_image_id "$collector_container")" || return 1
  tagged="$(image_id "$collector")" || return 1
  po_image_id_matches "$running" "$reviewed" && [[ "$running" == "$tagged" ]] || return 1
  ready="$(podman exec "$collector_container" ./bin/healthcheck --body "https://127.0.0.1:$port/ready")" || return 1
  collector_readiness_validate "$ready" || return 1
  collector_readiness_json_field "$ready" instance_id "\"$service\""
}
started_since() {
  local since_epoch="$1" container_name="$2"
  [[ "$(podman inspect -f '{{.State.StartedAt.Unix}}' "$container_name")" -ge "$since_epoch" ]]
}
require_post_cutover_logs() {
  local since="$1" container_name="$2" output status
  output="$(podman logs --since "$since" "$container_name" 2>&1)" || {
    status="$?"
    echo "failed to read post-cutover logs for $container_name" >&2
    return "$status"
  }
  if grep -Eq 'ERR|panic|permission denied|x509|no such file|OOM' <<<"$output"; then
    echo "post-cutover error in $container_name logs" >&2
    return 1
  else
    status="$?"
    [[ "$status" -eq 1 ]] || return "$status"
  fi
}
restore_collector_base_unit() {
  local recorded="$profile_release/previous/unit.container" installed="$unit_dir/$collector_container.container" tmp
  [[ -f "$recorded" ]] || { echo "[WARN] $recorded is missing; the installed collector unit definition stays as is" >&2; return 0; }
  ! cmp -s -- "$recorded" "$installed" || return 0
  cp -p -- "$installed" "$profile_release/rollback-replaced-$collector_container.container" || return "$?"
  tmp="$(mktemp "$unit_dir/.$collector_container.container.XXXXXX")" || return "$?"
  if ! install -m 0644 -- "$recorded" "$tmp" || ! writer_change mv -f -- "$tmp" "$installed"; then
    rm -f -- "$tmp"
    return 1
  fi
  echo "[ROLLBACK] restored the collector base unit the previous image ran with; reinstall the intended revision's unit before the next deployment" >&2
}
collector_origin_endpoint() {
  local installed="$unit_dir/$collector_container.container" recorded="$profile_release/previous/unit.container" env_file=-
  [[ "$slot" != c ]] || env_file="${compose_env:-/etc/stack-secrets/hololive-bot/compose.env}"
  [[ -f "$recorded" ]] || return 1
  postgres_endpoint_quadlet "$recorded" "$env_file" "$installed"
}
collector_rollback_endpoint_validate() {
  local installed="$unit_dir/$collector_container.container" recorded="$profile_release/previous/unit.container"
  local env_file=- current_endpoint previous_endpoint
  [[ "$slot" != c ]] || env_file="${compose_env:-/etc/stack-secrets/hololive-bot/compose.env}"
  [[ -f "$recorded" ]] || recorded="$installed"
  current_endpoint="$(postgres_endpoint_quadlet "$installed" "$env_file")" || return "$?"
  previous_endpoint="$(postgres_endpoint_quadlet "$recorded" "$env_file" "$installed")" || return "$?"
  postgres_endpoint_require_same "$current_endpoint" "$previous_endpoint" || return "$?"
  [[ -s "$backup/collector.endpoint" ]] || return 1
  postgres_endpoint_require_same "$current_endpoint" "$(<"$backup/collector.endpoint")"
}

cutover_units_settled() {
  local unit observed key value job_seen state_seen
  for unit in "$issuer_unit" "$collector_unit"; do
    observed="$(systemctl show --property=Job --property=ActiveState "$unit")" || return 1
    job_seen=false state_seen=false
    while IFS='=' read -r key value; do
      case "$key" in
        Job)
          [[ "$job_seen" == false && -z "$value" ]] || return 1
          job_seen=true
          ;;
        ActiveState)
          [[ "$state_seen" == false ]] || return 1
          state_seen=true
          case "$value" in active|inactive|failed) ;; *) return 1 ;; esac
          ;;
        *) return 1 ;;
      esac
    done <<<"$observed"
    [[ "$job_seen" == true && "$state_seen" == true ]] || return 1
  done
}
card_pair() {
  local inode
  inode="$(stat -c '%d:%i' "$backup")" || return 1
  printf '%s %s %s %s %s\n' "$backup" "$inode" "$1" "$2" "$3"
}

require_card_origin() {
  local release inode recorded_revision recorded_collector recorded_issuer extra
  read -r release inode recorded_revision recorded_collector recorded_issuer extra < "$1" || return 1
  local prefix="card-po-$slot"
  if [[ "$slot" == b && "${release##*/}" == staged-po-b-* ]]; then prefix=staged-po-b; fi
  [[ "${release%/*}" == "${backup%/*}" && "${release##*/}" =~ ^$prefix-[0-9]{8}T[0-9]{6}Z$ &&
     "$inode" =~ ^[0-9]+:[0-9]+$ && -z "$extra" && "$recorded_revision" == "$2" && "$recorded_collector" == "$3" &&
     "$recorded_issuer" =~ ^sha256:[0-9a-f]{64}$ && ( "$4" == absent || "$recorded_issuer" == "$4" ) ]] || return 1
  if [[ "$staged_pair" == true ]]; then
    [[ -f "$1" && ! -L "$1" && -d "$release" && ! -L "$release" && ! -L "$release/card-candidate" && "$(stat -c '%d:%i' "$release")" == "$inode" ]] &&
      cmp -s -- "$1" <(printf '%s %s %s %s %s\n' "$release" "$inode" "$recorded_revision" "$recorded_collector" "$recorded_issuer") &&
      cmp -s -- "$1" "$release/card-candidate" || return 1
  fi
}

require_card_candidate() {
  cmp -s -- "$backup/card-candidate" <(card_pair "$revision" "$(<"$staging/collector-image-id")" "$(<"$staging/image-id")") || return 1
  if [[ "$card_transition" == true ]]; then
    [[ "$(<"$backup/collector.revision")" != "$revision" &&
       "$(<"$backup/collector-prechange.image-id")" != "$(<"$staging/collector-image-id")" &&
       "$(<"$backup/po-prechange.image-id")" != "$(<"$staging/image-id")" ]] || return 1
  elif [[ "$staged_pair" == true ]]; then
    require_card_origin "$backup/card-origin" "$(<"$backup/collector.revision")" \
      "$(<"$backup/collector-prechange.image-id")" "$(<"$backup/po-prechange.image-id")" || return 1
  fi
  if [[ "$mode" == resume ]]; then
    postgres_endpoint_require_same "$(<"$backup/collector.endpoint")" "$(collector_origin_endpoint)" || return 1
  fi
}

claim_staged_attempt() {
  (set -C; printf '%s\n' "$seal" > "$backup/$mode-requested") && sync -- "$backup/$mode-requested" "$backup"
}

publish_card_pair() {
  local tmp
  tmp="$(mktemp -- "$remote_root/.po-$slot.card-active.XXXXXX")" || return 1
  cp -- "$1" "$tmp" && sync -- "$tmp" && writer_change mv -fT -- "$tmp" "$card_active" && sync -- "$remote_root"
}

restore() {
  local old_revision old_po_revision state issuer_now
  if [[ "$hold_owned" != true ]]; then trap - ERR INT TERM HUP; fi
  [[ -r "$backup/snapshot-complete" ]] || { echo "$host_label rollback snapshot incomplete" >&2; return 1; }
  if [[ "$mode" == rollback && "$hold_owned" != true ]] && ! cmp -s -- "$backup/card-candidate" "$card_active"; then
    echo 'rollback does not own the active card release' >&2; return 1
  fi
  if ! { [[ "$card_transition" == false && -f "$backup/card-origin" ]] &&
    { cmp -s -- "$backup/card-origin" "$card_active" || cmp -s -- "$backup/card-candidate" "$card_active"; }; }; then
    echo 'rollback is not owned by an admitted card pair' >&2; return 1
  fi
  if ! cutover_units_settled; then
    printf 'outcome_unknown\n' > "$backup/cutover-outcome" || echo 'failed to record cutover outcome; retain the backup' >&2
    echo "$host_label cutover outcome_unknown; settle issuer/collector manager jobs before an approved rollback; retain $backup" >&2
    return 1
  fi
  state="$(cat "$backup/issuer.state")"
  old_revision="$(cat "$backup/collector.revision")"
  verify_collector "$collector_rollback" "$backup/collector-prechange-manifest.json" "$old_revision" "$backup/collector-prechange.image-id"
  if [[ "$state" == present ]]; then
    old_po_revision="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_revision"])' "$backup/po-prechange-manifest.json")"
    po_verify_image "$issuer_rollback" "$backup/po-prechange-manifest.json" "$old_po_revision" arm64 "$backup/po-prechange.image-id"
    require_card_origin "$backup/card-origin" "$old_revision" "$(<"$backup/collector-prechange.image-id")" "$(<"$backup/po-prechange.image-id")" || return 1
  else
    [[ "$state" == absent ]] || return 1
    require_card_origin "$backup/card-origin" "$old_revision" "$(<"$backup/collector-prechange.image-id")" absent || return 1
  fi
  python3 "$pair_tool" verify "$service" "$profile_release/previous" "$collector_rollback"
  python3 "$pair_tool" check "$service" "$profile_release/previous"
  if ! collector_rollback_endpoint_validate; then
    if [[ "$mode" != rollback ]]; then
      if require_unmasked_units && create_hold && hold_units protective; then
        echo "[OUTCOME] held: endpoint transition prevents automatic rollback; retain $backup and $hold" >&2
      else
        echo "[OUTCOME] unconfirmed: endpoint transition prevents automatic rollback; retain $backup and $hold" >&2
      fi
    else
      echo "rollback refused before activation; current state unchanged; retain $backup and $profile_release" >&2
    fi
    return 1
  fi
  if [[ "$slot" == c ]]; then
    local previous_unit="$profile_release/previous/unit.container"
    [[ -f "$previous_unit" ]] || previous_unit="$unit_dir/$collector_container.container"
    postgres_capacity_assert_service "$staging" "$compose_env" "$unit_dir" "$collector_container" "$previous_unit" || return "$?"
  fi
  if [[ "$hold_owned" == true ]]; then require_held_units || return "$?"; fi
  if [[ "$staged_pair" == true ]]; then claim_staged_attempt || return 1; fi
  writer_change python3 "$pair_tool" activate "$service" "$profile_release/previous" "$current" || return "$?"
  restore_collector_base_unit || return "$?"
  writer_change podman tag "$collector_rollback" "$collector" || return "$?"
  writer_change systemctl daemon-reload || return "$?"
  if [[ "$state" == present ]]; then
    writer_change podman tag "$issuer_rollback" "$issuer" || return "$?"
    writer_change install -D -m 0644 "$backup/po-prechange-manifest.json" "$current/$manifest" || return "$?"
    writer_change install -D -m 0644 "$backup/po-prechange.image-id" "$current/$image_id_file" || return "$?"
    if [[ "$hold_owned" == true ]]; then
      release_unit "$issuer_unit"
    fi
    start_slot_unit "$issuer_unit"
    verify_issuer_runtime "$old_po_revision" "$(<"$backup/po-prechange.image-id")"
  else
    if [[ "$hold_owned" != true ]]; then systemctl stop "$issuer_unit"; fi
    rm -f "$current/$manifest" "$current/$image_id_file"
    issuer_now="$(issuer_presence)" || return 1
    if [[ "$issuer_now" == present ]]; then podman image rm "$issuer" >/dev/null; fi
  fi
  if [[ "$hold_owned" == true ]]; then
    if [[ "$state" == present ]]; then require_launched_unit "$issuer_unit"; fi
    release_unit "$collector_unit"
  fi
  start_slot_unit "$collector_unit" || return "$?"
  verify_collector_runtime "$old_revision" "$(<"$backup/collector-prechange.image-id")" || return "$?"
  python3 "$pair_tool" verify-live "$service" "$profile_release/previous" "$collector_container" || return "$?"
  postgres_endpoint_require_same "$(collector_database_endpoint "$collector_container")" "$(<"$backup/collector.endpoint")" || return "$?"
  publish_card_pair "$backup/card-origin" || return "$?"
  if [[ "$hold_owned" == true ]]; then
    if [[ "$state" == absent ]]; then release_unit "$issuer_unit"; fi
    clear_hold
    trap - ERR INT TERM HUP
  fi
  echo "$host_label previous collector/issuer restored from $backup"
}
collector_database_endpoint() {
  local vars key value
  local -A route=()
  vars="$(podman inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$1")" || return 1
  while IFS='=' read -r key value; do
    case "$key" in
      POSTGRES_HOST|POSTGRES_PORT|POSTGRES_DB|POSTGRES_SOCKET_PATH)
        [[ ! -v "route[$key]" ]] || return 1
        route[$key]="$value" ;;
    esac
  done <<< "$vars"
  [[ "${route[POSTGRES_DB]-}" == hololive && -z "${route[POSTGRES_SOCKET_PATH]-}" ]] || return 1
  postgres_endpoint_validate "${route[POSTGRES_HOST]-}" "${route[POSTGRES_PORT]-}"
}
require_collector_database_endpoint() {
  local expected='holo-postgres|5432' actual
  [[ "$slot" != b ]] || expected='100.100.1.8|5433'
  actual="$(collector_database_endpoint "$collector_container")" || return 1
  [[ "$actual" == "$expected" ]] || { echo 'activated collector must use the reviewed direct TLS endpoint' >&2; return 1; }
}
require_collector_lease_migration() {
  local position="$1" observed endpoint proof
  local migrator_user postgres_image owner leftover status=0
  . "$staging/scripts/deploy/lib/migration-receipts.sh"
  if [[ "$position" == applied ]]; then
    endpoint="$(collector_database_endpoint "$collector_container")" || return 1
    if [[ "$endpoint" != 'holo-postgres|5432' ]]; then
      echo 'collector migration proof requires the local primary route' >&2
      return 1
    fi
  fi
  proof="$(migration_collector_proof "$staging" "$position")" || return 1
  migrator_user="$(awk -F= '$1 == "HOLOLIVE_MIGRATOR_USER" { v = substr($0, length($1) + 2); gsub(/^["\047]|["\047]$/, "", v); print v }' \
    /etc/stack-secrets/hololive-bot/compose.env)"
  migrator_user="${migrator_user:-hololive_migrator}"
  [[ "$migrator_user" =~ ^[a-z_][a-z0-9_]*$ ]] || { echo 'collector migration proof requires a valid migrator role' >&2; return 1; }
  postgres_image="$(podman inspect -f '{{.Image}}' holo-postgres)" || return 1
  [[ "$postgres_image" =~ ^[0-9a-f]{64}$ ]] || { echo 'collector migration proof client identity is invalid' >&2; return 1; }
  owner="hololive.collector-migration-proof=$(cat /proc/sys/kernel/random/uuid)"
  observed="$(timeout 20 podman run -i --rm --label "$owner" --pull=never --read-only --cap-drop=all \
    --security-opt=no-new-privileges --network none \
    --env PGHOST=/var/run/postgresql --env PGPORT=5432 --env PGDATABASE=hololive --env "PGUSER=$migrator_user" \
    --env PGSSLMODE=disable --env PGCONNECT_TIMEOUT=5 --env "PGAPPNAME=$collector_container-migration-proof" \
    --env 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s' \
    --volume hololive-bot_holo-pg-admin-socket:/var/run/postgresql:ro \
    --entrypoint psql "$postgres_image" --no-psqlrc --quiet -v ON_ERROR_STOP=1 -At -f - <<<"$proof")" \
    || status="$?"
  leftover="$(podman ps --all --quiet --filter "label=$owner")" || return 1
  # shellcheck disable=SC2086 # container ID 목록을 인자로 나눈다.
  [[ -z "$leftover" ]] || podman rm --force $leftover >/dev/null
  [[ "$status" -eq 0 ]] || { echo 'collector migration proof query failed' >&2; return 1; }
  if [[ "$observed" != "on|t" ]]; then
    echo "collector requires a verified completed migration lineage; hold the paired cutover" >&2
    return 1
  fi
}

{
claim_slot
if [[ "$staged_pair" == true && "$mode" != quiesce ]]; then
  require_card_origin "$backup/card-origin" "$w2_revision" "$w2_collector" "$w2_issuer"
fi
if [[ "$mode" == rollback || "$mode" == check ]]; then require_card_candidate; fi
if [[ "$mode" == rollback ]]; then
  if [[ "$hold_owned" == true ]]; then
    require_confirmed_hold
    arm_maintenance_failure
  fi
  restore
  exit 0
fi
if [[ "$mode" == check ]]; then
  [[ -r "$backup/snapshot-complete" && -r "$current/$manifest" ]]
  po_verify_image "$issuer" "$current/$manifest" "$revision" arm64 "$current/$image_id_file"
  verify_collector "$collector" "$staging/collector-manifest.json" "$revision" "$staging/collector-image-id"
  [[ "$(image_label "$issuer" version)" == "$version" ]]
  [[ "$(image_label "$collector" version)" == "$version" ]]
  verify_issuer_runtime "$revision" "$(<"$staging/image-id")"
  verify_collector_runtime "$revision" "$(<"$staging/collector-image-id")"
  require_collector_database_endpoint
  python3 "$pair_tool" verify-live "$service" "$profile_release/candidate" "$collector_container"
  since="$(cat "$backup/change-started-at")"
  [[ "$since" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]
  since_epoch="$(date -u -d "$since" +%s)"
  for container in "$issuer_container" "$collector_container"; do
    started_since "$since_epoch" "$container"
    require_post_cutover_logs "$since" "$container"
    if [[ "$staged_pair" == true ]]; then
      read -r requested job identity < "$backup/resume-$container.service.start"
      [[ "$requested" =~ ^@[0-9]+\.[0-9]{6}$ && "$job" =~ ^[1-9][0-9]*$ &&
         "$(started_slot_generation "$container.service" "$requested" "$job")" == "$identity" && -n "$identity" ]]
    fi
  done
  echo "$host_label collector/issuer completion verified revision=$revision backup=$backup"
  exit 0
fi
cutover_units_settled || {
  echo "$host_label issuer/collector manager jobs are unsettled or unavailable; no paired cutover started" >&2
  exit 1
}
if [[ "$slot" == c && "$mode" != quiesce ]]; then
  postgres_capacity_assert_installed "$staging" "$compose_env" "$unit_dir"
fi
if [[ "$mode" != resume ]]; then
require_unmasked_units
systemctl is-active --quiet "$collector_unit" || {
  echo "$host_label collector must be running for a paired cutover; $collector_unit is not active" >&2
  exit 1
}
if [[ "$slot" == c ]]; then
  require_collector_lease_migration applied
fi
[[ -r "$staging/image.tar" && -r "$staging/image.tar.sha256" && -r "$staging/image-id" &&
   -r "$staging/collector-image.tar" && -r "$staging/collector-image.tar.sha256" && -r "$staging/collector-image-id" &&
   -r "$staging/rootfs-manifest.json" && -r "$staging/collector-manifest.json" ]] || exit 1
issuer_state="$(issuer_presence)"
if [[ "$mode" == quiesce && "$issuer_state" != present ]]; then
  echo 'quiesce requires the running previous issuer pair' >&2
  exit 1
fi
if [[ "$issuer_state" == present ]] && [[ ! -r "$current/$manifest" || ! -r "$current/$image_id_file" ]]; then
  echo "$host_label issuer receipt missing: restore the reviewed $current/$manifest and $current/$image_id_file before cutover" >&2
  exit 1
fi
[[ ! -e "$backup" ]] || { echo "$host_label backup path already exists" >&2; exit 1; }
if [[ "$card_transition" == false ]]; then
  previous_issuer=absent
  if [[ "$issuer_state" == present ]]; then previous_issuer="$(container_image_id "$issuer_container")"; fi
  require_card_origin "$card_active" "$(image_label "$collector" revision)" "$(container_image_id "$collector_container")" "$previous_issuer" || {
    echo 'running pair does not own the active card release' >&2; exit 1;
  }
  if [[ "$staged_pair" == true ]]; then
    require_card_origin "$card_active" "$w2_revision" "$w2_collector" "$w2_issuer"
    collector_invocation="$(active_invocation "$collector_unit")"
    captured_generation="$(container_generation "$collector_container")"
    issuer_invocation="$(active_invocation "$issuer_unit")"
    captured_issuer="$(container_generation "$issuer_container")"
    origin_snapshot="$(cat -- "$card_active" && printf 'collector %s %s\nissuer %s %s\n' \
      "$collector_invocation" "$captured_generation" "$issuer_invocation" "$captured_issuer")"
    observed_origin="$(printf '%s\n' "$origin_snapshot" | sha256sum)"
    [[ "${observed_origin%% *}" == "$origin_digest" ]] || { echo 'approved origin publisher generation changed' >&2; exit 1; }
    verify_collector_runtime "$w2_revision" "$w2_collector"
    verify_issuer_runtime "$w2_revision" "$w2_issuer"
    for tag in "$collector_rollback" "$issuer_rollback"; do
      status=0; podman image exists "$tag" || status=$?
      [[ "$status" == 1 ]] || { echo 'rollback image name already exists or inventory unavailable' >&2; exit 1; }
    done
    archives=()
    for prefix in '' collector-; do
      if [[ -n "$prefix" ]]; then tag="$collector_candidate" origin="$w2_collector"; else tag="$issuer_candidate" origin="$w2_issuer"; fi
      status=0; podman image exists "$tag" || status=$?
      case "$status" in
        0)
          tagged="$(image_id "$tag")"
          if [[ "$tagged" == "$(<"$staging/${prefix}image-id")" ]]; then continue; fi
          [[ "$tagged" == "$origin" ]] || { echo 'unexpected candidate tag owner' >&2; exit 1; } ;;
        1) ;;
        *) exit 1 ;;
      esac
      archives+=("$staging/${prefix}image.tar")
    done
    [[ ! -e "$profile_release" && ! -L "$profile_release" ]]
    cmp -- "$staging/$slot_unit_dir/$collector_container.container" "$unit_dir/$collector_container.container"
    [[ "$(collector_database_endpoint "$collector_container")" == '100.100.1.8|5433' ]] || {
      echo 'running b collector does not use the direct endpoint 100.100.1.8:5433' >&2; exit 1;
    }
  fi
fi
if [[ "$staged_pair" == true ]]; then (( EPOCHSECONDS < expires )); fi
install -d -m 0700 -o root -g root "$backup"
if [[ "$staged_pair" == true ]]; then
  cp -- /proc/sys/kernel/random/boot_id "$backup/owner-boot"
  printf '%s\n' "$tool_revision" > "$backup/owner-tool"
  printf '%s %s\n' "$window" "$expires" > "$backup/owner-window"
  printf '%s\n' "$origin_snapshot" > "$backup/owner-origin"
fi
old_image_id="$(image_id "$collector")"
running_image_id="$(container_image_id "$collector_container")"
[[ "$running_image_id" == "$old_image_id" ]] || exit 1
old_revision="$(image_label "$collector" revision)"
[[ "$old_revision" =~ ^[0-9a-f]{40}$ ]] || exit 1
printf '%s\n' "$old_image_id" > "$backup/collector-prechange.image-id"
python3 - "$old_revision" "$old_image_id" "$(image_label "$collector" version)" "$backup/collector-prechange-manifest.json" <<'PY'
import json, sys
json.dump({'schema_version': 1, 'source_revision': sys.argv[1], 'go': {'goarch': 'arm64'}, 'image_id': sys.argv[2], 'version': sys.argv[3]}, open(sys.argv[4], 'w'))
PY
verify_collector "$collector" "$backup/collector-prechange-manifest.json" "$old_revision" "$backup/collector-prechange.image-id"
if [[ "$mode" == quiesce && "$staged_pair" == false ]]; then
  collector_invocation="$(active_invocation "$collector_unit")"
  issuer_invocation="$(active_invocation "$issuer_unit")"
  captured_issuer="$(container_generation "$issuer_container")"
fi
python3 "$pair_tool" capture "$service" "$profile_release/previous" "$collector_container" "$current"
python3 "$pair_tool" verify "$service" "$profile_release/previous" "$old_image_id"
if [[ "$mode" == quiesce ]]; then
  [[ -f "$profile_release/previous/unit.container" ]] || { echo 'previous pair has no retained base unit' >&2; exit 1; }
fi
captured_collector="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["container_id"])' "$profile_release/previous/pair.json")"
[[ "$captured_collector" =~ ^[0-9a-f]{64}$ ]]
if [[ "$staged_pair" == true ]]; then [[ "$captured_collector" == "${captured_generation%% *}" ]]; fi
collector_database_endpoint "$captured_collector" > "$backup/collector.endpoint"
if [[ "$seal" != - ]]; then
  printf '%s\n' "$seal" > "$backup/transport.sha256"
fi
if [[ "$card_transition" == false ]]; then
  cp -- "$card_active" "$backup/card-origin"
fi
writer_change python3 "$pair_tool" rollback "$service" "$profile_release/previous"
python3 "$pair_tool" check "$service" "$profile_release/previous"
printf '%s\n' "$old_revision" > "$backup/collector.revision"
writer_change podman tag "$old_image_id" "$collector_rollback"
if [[ "$issuer_state" == present ]]; then
  running_issuer_id="$(container_image_id "$issuer_container")"
  issuer_image_id="$(image_id "$issuer")"
  [[ "$running_issuer_id" == "$issuer_image_id" ]] || exit 1
  old_po_revision="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_revision"])' "$current/$manifest")"
  po_verify_image "$issuer" "$current/$manifest" "$old_po_revision" arm64 "$current/$image_id_file"
  if [[ "$mode" == quiesce ]]; then verify_issuer_runtime "$old_po_revision" "$issuer_image_id"; fi
  [[ "$old_po_revision" == "$old_revision" ]]
  old_po_version="$(image_label "$issuer" version)"
  old_version="$(image_label "$collector" version)"
  [[ -n "$old_version" && "$old_po_version" == "$old_version" ]]
  cp "$current/$manifest" "$backup/po-prechange-manifest.json"
  cp "$current/$image_id_file" "$backup/po-prechange.image-id"
  writer_change podman tag "$issuer_image_id" "$issuer_rollback"
  printf 'present\n' > "$backup/issuer.state"
else
  [[ ! -e "$current/$manifest" && ! -e "$current/$image_id_file" ]]
  printf 'absent\n' > "$backup/issuer.state"
fi
printf '%s\n' "$revision" > "$backup/candidate.revision"
printf '%s\n' "$version" > "$backup/candidate.version"
card_pair "$revision" "$(<"$staging/collector-image-id")" "$(<"$staging/image-id")" > "$backup/card-candidate"
require_card_candidate
if [[ "$card_transition" == false ]]; then
  require_card_origin "$backup/card-origin" "$old_revision" "$old_image_id" "${issuer_image_id:-absent}"
fi
(cd "$staging" && sha256sum image-id collector-image-id rootfs-manifest.json collector-manifest.json \
  image.tar.sha256 collector-image.tar.sha256 source-files.sha256) > "$backup/candidate-inputs.sha256"
: > "$backup/snapshot-complete"
if [[ "$staged_pair" == true ]]; then sync -- "$backup"/* "$backup" "${backup%/*}"; fi
fi
restore_after_failed_cutover() {
  local status="${1:-1}" restore_status
  if [[ "$BASHPID" != "${cutover_restore_owner_pid:?cutover restore owner not armed}" ]]; then
    exit "$status"
  fi
  trap - ERR INT TERM HUP
  set +e
  if [[ "$cutover_started" == true ]]; then
    (set -e; restore)
    restore_status="$?"
  else
    restore_status=0
  fi
  if [[ "$restore_status" -ne 0 ]]; then
    echo "$host_label automatic restoration failed; retain $backup" >&2
  fi
  exit "$status"
}
if [[ "$mode" != resume ]]; then
  if [[ "$staged_pair" == true ]]; then
    (( EPOCHSECONDS < expires ))
  else
    cutover_restore_owner_pid="$BASHPID"
    trap 'restore_after_failed_cutover $?' ERR
    trap 'restore_after_failed_cutover 130' INT
    trap 'restore_after_failed_cutover 143' TERM
    trap 'restore_after_failed_cutover 129' HUP
    archives=("$staging/image.tar" "$staging/collector-image.tar")
  fi
  for archive in "${archives[@]}"; do writer_change podman load --input "$archive"; done
  verify_candidate_images
fi
if [[ "$mode" == quiesce ]]; then
  if [[ "$staged_pair" == true ]]; then
    validate_candidate_pair
    (cd -- "$profile_release/candidate" && sha256sum profile.json unit.container 50-worker-profile.conf) > "$backup/candidate-runtime.sha256"
    sync -- "$profile_release/candidate"/* "$profile_release/candidate" "$backup/candidate-runtime.sha256" "$backup"
    cmp -s -- "$card_active" "$backup/card-origin"
    cmp -s -- "$card_active" <(head -n 1 -- "$backup/owner-origin")
    require_card_origin "$card_active" "$w2_revision" "$w2_collector" "$w2_issuer"
    [[ "$(container_generation "$collector_container")" == "$captured_generation" ]]
  fi
  require_unmasked_units
  verify_issuer_runtime "$old_po_revision" "$issuer_image_id" || {
    echo 'captured issuer is no longer healthy on its reviewed image and socket; no hold created' >&2; exit 1;
  }
  [[ "$(container_generation "$issuer_container")" == "$captured_issuer" &&
     "$(active_invocation "$issuer_unit")" == "$issuer_invocation" &&
     "$(active_invocation "$collector_unit")" == "$collector_invocation" ]] || {
    echo 'captured writer generation changed during image loading; no hold created' >&2; exit 1;
  }
  if [[ "$staged_pair" == false ]]; then captured_generation="$(container_generation "$collector_container")"; fi
  python3 "$pair_tool" verify-captured "$service" "$profile_release/previous" "$collector_container"
  if [[ "$staged_pair" == true ]]; then (( EPOCHSECONDS < expires )); fi
  arm_maintenance_failure
  create_hold
  hold_units initial
  trap - ERR INT TERM HUP
  printf '%s quiesced: both writers persistently held; backup=%s\n' "$host_label" "$backup"
  exit 0
fi
if [[ "$mode" == resume ]]; then
  require_confirmed_hold
  arm_maintenance_failure
  if [[ "$staged_pair" == true ]]; then
    claim_staged_attempt
    (cd -- "$profile_release/candidate" && sha256sum --check --strict "$backup/candidate-runtime.sha256")
  fi
  (cd "$staging" && sha256sum --check --strict image.tar.sha256 collector-image.tar.sha256)
  verify_candidate_images
fi
validate_candidate_pair
if [[ "$slot" == c ]]; then require_collector_lease_migration target; fi
if [[ "$staged_pair" == true ]]; then (( EPOCHSECONDS < expires )); fi
cutover_started=true
writer_change python3 "$pair_tool" activate "$service" "$profile_release/candidate" "$current"
writer_change podman tag "$issuer_candidate" "$issuer"
writer_change podman tag "$collector_candidate" "$collector"
if [[ "$mode" == resume ]]; then
  writer_change install -D -m 0644 "$staging/rootfs-manifest.json" "$current/$manifest"
  writer_change install -D -m 0644 "$staging/image-id" "$current/$image_id_file"
fi
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf '%s\n' "$started_at" > "$backup/change-started-at"
writer_change systemctl daemon-reload
if [[ "$mode" == resume ]]; then
  require_held_units
  python3 "$pair_tool" verify-source "$service" "$profile_release/candidate"
  cmp -- "$profile_release/candidate/unit.container" "$unit_dir/$collector_container.container"
  release_unit "$issuer_unit"
fi
start_slot_unit "$issuer_unit"
verify_issuer_runtime "$revision" "$(<"$staging/image-id")"
if [[ "$mode" != resume ]]; then
  writer_change install -D -m 0644 "$staging/rootfs-manifest.json" "$current/$manifest"
  writer_change install -D -m 0644 "$staging/image-id" "$current/$image_id_file"
fi
if [[ "$mode" == resume ]]; then require_launched_unit "$issuer_unit"; release_unit "$collector_unit"; fi
start_slot_unit "$collector_unit"
verify_collector_runtime "$revision" "$(<"$staging/collector-image-id")"
require_collector_database_endpoint
python3 "$pair_tool" verify-live "$service" "$profile_release/candidate" "$collector_container"
since_epoch="$(date -u -d "$started_at" +%s)"
for container in "$issuer_container" "$collector_container"; do
  started_since "$since_epoch" "$container"
  require_post_cutover_logs "$started_at" "$container"
done
if [[ "$staged_pair" == true ]]; then
  require_launched_unit "$issuer_unit"
  require_launched_unit "$collector_unit"
  cmp -s -- "$backup/card-origin" "$card_active"
fi
publish_card_pair "$backup/card-candidate"
if [[ "$mode" == resume ]]; then clear_hold; fi
trap - ERR INT TERM HUP
printf '%s issuer-first cutover verified revision=%s backup=%s started_at=%s\n' "$host_label" "$revision" "$backup" "$started_at"
}
