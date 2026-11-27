package quadlet

var fakes = map[string]string{
	"hostname": `echo hololive-seoul
`,
	"mkdir": `command -p mkdir "$@" || exit
if [[ "$FAKE_CASE" == term_before_first_unmask && "${*: -1}" == */opening ]]; then kill -TERM "$PPID"; fi
`,
	"mktemp": `[[ "$FAKE_CASE" != rehold_evidence_unwritable || "${*: -1}" != */rehold.XXXXXX ]] || exit 1
command -p mktemp "$@"
`,
	"podman": `state="${FAKE_STATE:?}"
if [[ "$1 $2" == 'exec -i' ]]; then
  [[ "$3" == holo-postgres ]] || exit 2
  if [[ "$4" == env ]]; then
    if [[ " $* " == *' PGAPPNAME=hololive-writers-draining '* ]]; then
      while IFS= read -r line; do printf 't\n'; done
    elif [[ " $* " == *' PGAPPNAME=hololive-writers-gate '* ]]; then
      grep -q 'CREATE EVENT TRIGGER hololive_writer_admission ON login' || exit 3
      printf 'writers-gate\n' >>"$state/calls"
      [[ "$FAKE_CASE" != gate_failure ]] || exit 1
    else
      cat >/dev/null
      [[ "$FAKE_CASE" != single_before_card && "$FAKE_CASE" != idle_frontend ]] || { printf 'f\n'; exit 0; }
      printf 't\n'
    fi
    exit 0
  fi
  cat >/dev/null
  role="${*: -1}"
  printf 'writer-sql %s\n' "$role" >>"$state/calls"
  [[ "${FAKE_CASE}" != "sql_failure_$role" ]] || exit 1
  printf 't\n'
  exit 0
fi
printf 'podman %s\n' "$*" >>"${state}/calls"
image_file() { printf -v "$1" '%s/images/%s' "${state}" "${2//[\/:]/_}"; }
case "$1 ${2:-}" in
  'info --format')
    case "$3" in
      '{{.Host.Arch}}') printf 'arm64\n' ;;
      *) exit 2 ;;
    esac ;;
  'inspect -f')
    [[ "${*: -1}" =~ ^([0-9a-f]{64}|hololive-api|hololive-alarm-worker)$ ]] || exit 2
    if [[ "$3" == '{{.Id}}' || "$3" == '{{.State.Running}} {{.Id}} {{.State.ConmonPid}}' ]]; then
      if [[ -f "$state/signal-at-capture" ]]; then
        rm "$state/signal-at-capture"
        kill -TERM -- "-${FAKE_SIGNAL_OWNER:?}"
      fi
      cid="$(<"$state/$4/cid")" || exit 1
      if [[ "$3" == '{{.Id}}' ]]; then printf '%s\n' "$cid"; else
        printf '%s %s %s\n' "$(<"$state/$4/active")" "$cid" "$(<"$state/$4/conmonpid")"
      fi
      exit
    fi
    cat "${state}/running-endpoint"
    ;;
  'container exists')
    [[ "${FAKE_COORDINATED:-false}" == true ]] || exit 2
    [[ "${FAKE_CASE}" != inventory_failure ]] || exit 125
    if [[ "$3" =~ ^[0-9a-f]{64}$ ]]; then
      [[ ! -f "$state/residual-cid" || "$(<"$state/residual-cid")" != "$3" ]] || exit 0
      for peer in hololive-alarm-worker hololive-api; do
        [[ "$(<"$state/$peer/active")" != true || "$(<"$state/$peer/cid")" != "$3" ]] || exit 0
      done
      exit 1
    fi
    [[ "$(<"$state/$3/active")" == true || "${FAKE_CASE}" == residual_container ]]
    ;;
  'container inspect')
    [[ "$3" == --format && "$4" == '{{.State.Running}} {{.Id}} {{.Image}} {{.State.StartedAt.UnixNano}}' ]] || exit 2
    case "$5" in
      holo-postgres) printf 'true %064d %064d %s\n' 91 81 "$(<"$state/pg-started")" ;;
      *) exit 2 ;;
    esac ;;
  'image exists') image_file file "$3"; [[ -f "$file" ]] ;;
  'image inspect')
    image_file file "${*: -1}"
    [[ -f "${file}" ]] || exit 125
    case "$4" in
      '{{.Id}}') key=id ;;
      '{{.Os}}/{{.Architecture}}') key=platform ;;
      *io.hololive-bot.outbox-schema*)
        if grep -qx 'schema=text-file-v1' "$file"; then echo true; else echo false; fi
        exit 0 ;;
      *image.title*) key=title ;;
      *image.revision*) key=revision ;;
      *) exit 2 ;;
    esac
    sed -n "s/^${key}=//p" "${file}"
    ;;
  *) exit 2 ;;
esac
`,
	"journalctl": `set -eu
if [[ "$6" == --output-fields=INVOCATION_ID ]]; then
  [[ "$1 $2 $3 $4 $5" == '--boot --quiet --no-pager -o json' && "$7" =~ ^--since=@[0-9]+\.[0-9]{6}$ &&
    "$8" == _PID=1 && "$9" == UNIT=*.service && "${10}" =~ ^JOB_ID=[1-9][0-9]*$ &&
    "${11} ${12} ${13}" == 'JOB_TYPE=start JOB_RESULT=done MESSAGE_ID=39f53479d3a045ac8e11786248231fbf' ]] || exit 2
  unit="${9#UNIT=}"
  printf 'journal start %s %s\n' "$unit" "${10#JOB_ID=}" >>"$FAKE_STATE/calls"
  current="$FAKE_STATE/${unit%.service}"
  [[ -f "$current/start-done" ]] || exit 0
  read -r at job invocation <"$current/start-done"
  [[ ! "$at" < "${7#--since=@}" && "$job" == "${10#JOB_ID=}" ]] || exit 0
  printf '{"INVOCATION_ID":"%s"}\n' "$invocation"
  exit 0
fi
[[ "$1 $2 $3 $4 $5 $6" == '--boot --quiet --no-pager -o json --output-fields=INVOCATION_ID,MESSAGE_ID,JOB_ID,JOB_RESULT' &&
  "$7" =~ ^--since=@[0-9]+\.[0-9]{6}$ && "$8" == _PID=1 && "$9" == UNIT=*.service &&
  "${10}" == INVOCATION_ID=* && "${11}" == JOB_TYPE=stop && "${12}" == MESSAGE_ID=de5b426a63be47a7b6ac3eaac82e2f6f &&
  "${13}" == MESSAGE_ID=9d1aaa27d60140bd96365438aad20286 ]] || exit 2
unit="${9#UNIT=}"
id="${10#INVOCATION_ID=}"
printf 'journal stop %s\n' "$unit" >>"$FAKE_STATE/calls"
[[ "$FAKE_CASE" != journal_failure ]] || exit 1
read -r actual at <"$FAKE_STATE/${unit%.service}/stopping" || exit 1
[[ "$FAKE_CASE" != missing_job && "$id" == "$actual" && ! "$at" < "${7#--since=@}" ]] || exit 0
printf '{"INVOCATION_ID":"%s","MESSAGE_ID":"%s","JOB_ID":"12"}\n' "$actual" "${12#MESSAGE_ID=}"
job=12
[[ "$FAKE_CASE" != other_done_job ]] || job=13
printf '{"INVOCATION_ID":"%s","MESSAGE_ID":"%s","JOB_ID":"%s","JOB_RESULT":"done"}\n' "$actual" "${13#MESSAGE_ID=}" "$job"
`,
	"systemctl": `state="${FAKE_STATE:?}"
case "$*" in
  show-environment)
    [[ ! -f "${state}/manager-environment" ]] || cat "${state}/manager-environment"
    exit 0 ;;
  'show --property=UnitPath --value')
    printf '%s\n' "${HOLOLIVE_QUADLET_TEST_ROOT}/etc/systemd/system" "${HOLOLIVE_QUADLET_TEST_ROOT}/custom-systemd"
    exit 0 ;;
  show\ *.service\ --property=NeedDaemonReload\ --value)
    if [[ -e "${state}/manager-reload-required" ]]; then echo yes; else echo no; fi
    exit 0 ;;
  show\ *.service\ --property=Names\ --value)
    printf '%s\n' "$2"
    exit 0 ;;
  show\ *.service\ --property=DropInPaths\ --value)
    exit 0 ;;
  show\ *.service\ --property=LoadState\ --value)
    if [[ -L "${HOLOLIVE_QUADLET_TEST_ROOT}/etc/systemd/system/$2" ]]; then echo masked; else echo loaded; fi
    exit 0 ;;
  show\ *.service\ --property=FragmentPath\ --value)
    if [[ -L "${HOLOLIVE_QUADLET_TEST_ROOT}/etc/systemd/system/$2" ]]; then echo "/etc/systemd/system/$2"; else echo "/run/systemd/generator/$2"; fi
    exit 0 ;;
esac
launch_job=''
if [[ "$1" == --show-transaction ]]; then
  [[ "$#" == 3 && "$2" == start && "${FAKE_COORDINATED:-false}" == true &&
    "$LC_ALL $SYSTEMD_LOG_COLOR $SYSTEMD_LOG_LEVEL $SYSTEMD_LOG_TARGET" == 'C 0 info console' ]] || exit 2
  case "$3" in
    hololive-alarm-worker.service) launch_job=71 ;;
    hololive-api.service) launch_job=72 ;;
    *) exit 2 ;;
  esac
  shift
fi
printf 'systemctl %s\n' "$*" >>"${state}/calls"
state_dir() {
  if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
    printf -v "$1" '%s/%s' "${state}" "${2%.service}"
  else
    printf -v "$1" '%s' "${state}"
  fi
}
restart_unit() {
  local unit="$1" current dropin pair worker next
  state_dir current "${unit}"
  if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
    [[ "$(<"${current}/loadstate")" != masked ]] || return 1
    [[ ! -e "$current/guard" ]] || return 0
    if [[ "${unit}" == hololive-api.service ]]; then
      state_dir worker hololive-alarm-worker.service
      if [[ "$(<"${worker}/active")" != true && "$(<"${worker}/loadstate")" != masked ]]; then
        restart_unit hololive-alarm-worker.service || return
      fi
    fi
  fi
  dropin="${FAKE_QUADLET_DIR}/${unit%.service}.container.d/50-worker-profile.conf"
  pair=none
  [[ ! -f "${dropin}" ]] || pair="$(sed -n 's/^pair=//p' "${dropin}")"
  printf '%s\n' "${pair}" >"${current}/runtime"
  printf '0\n' >"${current}/nrestarts"
  if [[ "${FAKE_SERVER_EPOCH:-old}" == new && "${pair}" != candidate ]]; then
    printf '%s\n' "${unit}" >>"${state}/unsafe-start"
  fi
  if [[ "${FAKE_CASE}" == restore_failure || ( "${FAKE_CASE}" == health_failure && "${pair}" == candidate ) ]]; then
    printf 'false\n' >"${current}/active"
    return 1
  fi
  if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
    next=$((10#$(<"$current/cid") + 10))
    printf '%032d\n' "$next" >"$current/invocation"
    printf '%064d\n' "$next" >"$current/cid"
    printf '%d\n' "$((1000 + next))" >"$current/mainpid"
    cp "$current/mainpid" "$current/conmonpid" || return 1
    if [[ -n "${2:-}" ]]; then
      printf '%s %s %s\n' "$EPOCHREALTIME" "$2" "$(<"$current/invocation")" >"$current/start-done"
    fi
  fi
  printf 'true\n' >"${current}/active"
  [[ "${FAKE_CASE}" != auto_restart || "${pair}" != candidate ]] || printf '1\n' >"${current}/nrestarts"
  if [[ "$unit" == hololive-alarm-worker.service ]]; then
    case "$FAKE_CASE" in
      term_after_first_restart)
        : >"$state/signal-at-capture"
        kill -TERM -- "-${FAKE_SIGNAL_OWNER:?}" ;;
      replaced_at_launch)
        printf '%032d\n' 5 >"$current/invocation"
        printf '%064d\n' 5 >"$current/cid"
        printf '1005\n' >"$current/mainpid"
        cp "$current/mainpid" "$current/conmonpid" || return 1
        printf '1\n' >"$current/nrestarts" ;;
      client_failed_done|client_failed_no_anchor) return 1 ;;
      client_failed_foreign_cid)
        printf '%064d\n' 5 >"$current/cid"
        printf '1005\n' >"$current/conmonpid"
        return 1 ;;
      client_failed_pending)
        printf 'false\n' >"$current/active"
        mv "$current/start-done" "$current/pending-event"
        return 1 ;;
      wrong_start_record)
        printf '%s 99 %s\n' "$EPOCHREALTIME" "$(<"$current/invocation")" >"$current/start-done" ;;
      restart_client_interrupted)
        printf '%032d\n' 5 >"$current/invocation"
        printf '%064d\n' 5 >"$current/cid"
        printf '1005\n' >"$current/mainpid"
        cp "$current/mainpid" "$current/conmonpid" || exit 2
        exec "$FAKE_REAL_PYTHON3" -c 'import os, signal
assert int(os.environ["FAKE_SIGNAL_OWNER"]) == os.getpgrp() == os.getsid(0)
signal.signal(signal.SIGTERM, signal.SIG_DFL)
os.killpg(os.getpgrp(), signal.SIGTERM)' ;;
    esac
  fi
}
case "$1" in
  daemon-reload)
    if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
      for service in hololive-api hololive-alarm-worker; do
        if [[ -L "$HOLOLIVE_QUADLET_TEST_ROOT/etc/systemd/system/$service.service" ]]; then
          printf 'masked\n' >"$state/$service/loadstate"
          printf 'masked\n' >"$state/$service/mask_scope"
          rm -f "$state/$service/exit" "$state/$service/failed"
        fi
      done
    fi
    ;;
  is-active) state_dir current "${*: -1}"; [[ "$(<"${current}/active")" == true ]] ;;
  show)
    state_dir current "$2"
    if [[ "$3" == --all ]]; then
      [[ "$4 $5" == '-p LoadState,ActiveState,InvocationID,Result,ExecMainCode,ExecMainStatus,MainPID,ControlPID' ]] || exit 2
      load="$(<"$current/loadstate")"
      active=inactive main_pid=0 result=success code=0 status=0
      if [[ "$(<"$current/active")" == true ]]; then active=active; main_pid=42; fi
      if [[ "$FAKE_CASE" == residual_pid ]]; then main_pid=99; fi
      if [[ -f "$current/failed" ]]; then active=failed; result=exit-code; fi
      if [[ -f "$current/exit" ]]; then read -r code status <"$current/exit"; fi
      printf 'LoadState=%s\nActiveState=%s\nInvocationID=%s\nResult=%s\nExecMainCode=%s\nExecMainStatus=%s\nMainPID=%s\nControlPID=0\n' \
        "$load" "$active" "$(<"$current/invocation")" "$result" "$code" "$status" "$main_pid"
      exit 0
    fi
    case "$4" in
      NRestarts) cat "${current}/nrestarts" ;;
      LoadState) cat "${current}/loadstate" ;;
      UnitFileState) cat "${current}/mask_scope" ;;
      ActiveState) if [[ "$(<"${current}/active")" == true ]]; then echo active; else echo inactive; fi ;;
      InvocationID) cat "${current}/invocation" ;;
      MainPID)
        if [[ "$(<"${current}/active")" == true && -f "$current/mainpid" ]]; then cat "$current/mainpid";
        elif [[ "$(<"${current}/active")" == true || "$FAKE_CASE" == residual_pid ]]; then echo 42; else echo 0; fi ;;
      ControlPID) echo 0 ;;
      Job)
        if [[ -f "$current/pending-event" ]]; then
          [[ -f "$current/guard" ]] || exit 2
          if [[ -f "$current/poll" ]]; then
            mv "$current/pending-event" "$current/start-done"
            printf 'true\n' >"$current/active"
          else
            : >"$current/poll"
            printf '71\n'
          fi
        fi ;;
      Result) if [[ -f "$current/failed" ]]; then echo exit-code; else echo success; fi ;;
      *) exit 2 ;;
    esac
    ;;
  stop)
    shift
    for unit in "$@"; do
      state_dir current "$unit"
      if [[ ! -f "$current/guard" || "$(<"$current/loadstate")" != loaded ]]; then
        printf '%s\n' "$unit" >>"$state/unguarded-stop"
      fi
      printf '%s %s\n' "$(<"$current/invocation")" "$EPOCHREALTIME" >"$current/stopping"
      status=0
      if [[ "$FAKE_CASE" == gc_exit_143 || "$FAKE_CASE" == failed_stop || "$FAKE_CASE" == failed_rehold ]]; then status=143; fi
      printf '%s %s %s\n' "$(<"$current/cid")" "$EPOCHREALTIME" "$status" >"$current/died"
      printf '1 0\n' >"$current/exit"
      if [[ "$FAKE_CASE" == failed_stop || ("$FAKE_CASE" == failed_rehold && "$unit" == hololive-alarm-worker.service) ]]; then
        : >"$current/failed"
        printf '1 143\n' >"$current/exit"
      elif [[ "$FAKE_CASE" == abnormal_success_result ]]; then
        printf '1 143\n' >"$current/exit"
      else
        : >"$current/invocation"
        rm -f "$current/exit"
      fi
      printf 'false\n' >"$current/active"
      if [[ "${FAKE_CASE}" == other_stopped_invocation ]]; then printf '%032d\n' 3 >"$current/invocation"; fi
      if [[ "$FAKE_CASE" == peer_changed ]]; then printf '%032d\n' 9 >"$state/hololive-api/invocation"; fi
    done
    [[ "$FAKE_CASE" != stop_command_failed ]]
    ;;
  unmask)
    shift
    if [[ "${1:-}" == --runtime ]]; then shift; fi
    for unit in "$@"; do
      state_dir current "${unit}"
      rm -- "$HOLOLIVE_QUADLET_TEST_ROOT/etc/systemd/system/$unit" || exit 1
      if [[ "$unit" == hololive-api.service ]]; then
        case "$FAKE_CASE" in
          unmask_reload_failed) exit 1 ;;
          unmask_with_residual_cid) cp "$current/cid" "$state/residual-cid"; exit 1 ;;
        esac
      fi
      printf 'loaded\n' >"${current}/loadstate"
      printf 'generated\n' >"${current}/mask_scope"
      if [[ "$FAKE_CASE" == term_after_unmask ]]; then kill -TERM "$PPID"; fi
    done
    ;;
  start)
    if [[ -n "$launch_job" ]]; then
      state_dir current "$2"
      [[ "$(<"$current/active")" == false && "$(<"$current/loadstate")" == loaded ]] || exit 2
      if [[ "$FAKE_CASE" != client_failed_no_anchor ]]; then
        printf 'Enqueued anchor job %s %s/start.\n' "$launch_job" "$2" >&2
      fi
      restart_unit "$2" "$launch_job"
      exit $?
    fi
    [[ "$2" == hololive-db-migrate.service ]] || exit 2
    if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
      for unit in hololive-api hololive-alarm-worker; do
        if [[ "$(<"${state}/${unit}/active")" == true ]]; then
          printf '%s\n' "${unit}" >>"${state}/unsafe-migration"
        fi
      done
    fi
    sed -n 's/^Image=//p' "${FAKE_QUADLET_DIR}/hololive-db-migrate.container.d/50-migrator-image.conf" >"${state}/migrated-with"
    if [[ "${FAKE_CASE}" == fence_during_migration ]]; then
      mkdir -p "${FAKE_FENCE_DIR}"
      : >"${FAKE_FENCE_DIR}/fence.intent"
    fi
    if [[ "$FAKE_CASE" == lost_hold_during_migration ]]; then printf 'true\n' >"$state/hololive-api/active"; fi
    [[ "${FAKE_CASE}" != migration_failure ]]
    ;;
  simulate-reboot)
    for unit in hololive-alarm-worker.service hololive-api.service; do
      state_dir current "${unit}"
      printf 'false\n' >"${current}/active"
      if [[ "$(<"${current}/mask_scope")" != masked ]]; then
        printf 'loaded\n' >"${current}/loadstate"
        restart_unit "${unit}" || exit
      fi
    done
    ;;
  restart) restart_unit "$2" ;;
  *) exit 2 ;;
esac
`,
	"python3": `state="${FAKE_STATE:?}"
if [[ "${1:-} ${2:-}" == '-I -B' && ( "${3:-}" == "${FAKE_PAIR_TOOL%/*}/systemd-stop-guard.py" ||
  "${3:-}" == "${FAKE_STAGE_PAIR_TOOL%/*}/systemd-stop-guard.py" ) ]]; then
  [[ -f "$3" && "$#" == 7 ]] || exit 2
  action="$4" unit="$5" anchor="$6" identity="$7"
  if [[ "$action" == exit-status ]]; then
    current="$state/${unit%.service}"
    printf 'exit-status %s\n' "$unit" >>"$state/calls"
    [[ "$6" =~ ^[0-9a-f]{64}$ && "$7" =~ ^@[0-9]+\.[0-9]{6}$ && "$FAKE_CASE" != missing_exit ]] || exit 1
    read -r cid at code <"$current/died" && [[ "$cid" == "$6" && ! "$at" < "${7#@}" ]] || exit 1
    printf '%s\n' "$code"
    exit 0
  fi
  current="$state/${unit%.service}" mask="${anchor%/*}/$unit"
  printf 'guard %s %s\n' "$action" "$unit" >>"$state/calls"
  [[ "$anchor" == "$HOLOLIVE_QUADLET_TEST_ROOT/etc/systemd/system/."*.mask && "$(readlink "$anchor")" == /dev/null &&
    "$(stat -c '%d:%i' "$anchor")" == "$identity" && ! -L "$mask" && ! -e "$mask" ]] || exit 1
  case "$action" in
    undo-unmask)
      [[ "$(<"$current/loadstate")" == masked && "$(<"$current/active")" == false &&
        ! -e "$current/guard" ]] || exit 1
      ln -PT -- "$anchor" "$mask" && systemctl daemon-reload || exit 1
      ;;
    arm)
      [[ "$FAKE_CASE" != partial_guard || "$unit" != hololive-api.service ]] || exit 1
      (set -C; printf '%s\n' "$identity" >"$current/guard") || exit 1
      [[ "$(<"$current/loadstate")" == loaded ]] || exit 1
      systemctl daemon-reload || exit 1
      if [[ "$FAKE_CASE" == invocation_changed ]]; then printf '%032d\n' 2 >"$current/invocation"; fi
      if [[ "$FAKE_CASE" == lost_guard && "$unit" == hololive-api.service ]]; then rm "$state/hololive-alarm-worker/guard"; fi
      ;;
    check|seal|start-budget)
      [[ "$(<"$current/loadstate")" == loaded ]] || exit 1
      [[ -f "$current/guard" && "$(<"$current/guard")" == "$identity" ]] || exit 1
      if [[ "$action" == start-budget ]]; then printf '180\n'; fi
      if [[ "$action" == seal ]]; then
        [[ "$(<"$current/active")" == false && "$FAKE_CASE" != residual_pid ]] || exit 1
        ln -PT -- "$anchor" "$mask" && systemctl daemon-reload && rm "$current/guard" || exit 1
      fi
      ;;
    *) exit 2 ;;
  esac
  exit 0
fi
[[ "${1:-}" == "${FAKE_PAIR_TOOL}" || "${1:-}" == "${FAKE_STAGE_PAIR_TOOL:-unused}" ]] || exec "${FAKE_REAL_PYTHON3:?}" "$@"
shift
action="$1" service="$2" dir="$3"
printf 'pair %s %s %s\n' "${action}" "${service}" "${dir##*/}" >>"${state}/calls"
unit_dropin="${FAKE_QUADLET_DIR}/${service}.container.d/50-worker-profile.conf"
current="${state}"
if [[ "${FAKE_COORDINATED:-false}" == true ]]; then current="${state}/${service}"; fi
case "${action}" in
  capture|prepare)
    [[ "$4" == "${service}" && "$5" == "${FAKE_DEPLOY_ROOT}" ]] || exit 3
    [[ "$(<"${current}/active")" == true ]] || exit 3
    mkdir "${dir}"
    printf -v cid '%064d' 1
    if [[ "${FAKE_COORDINATED:-false}" == true ]]; then cid="$(<"$current/cid")"; fi
    printf '{"container_id":"%s","image":"sha256:%064d"}\n' "$cid" 8 >"${dir}/pair.json"
    printf 'profile\n' >"${dir}/profile.json"
    if [[ -f "${state}/previous-unit" ]]; then
      cp "${state}/previous-unit" "${dir}/unit.container"
    else
      cp "${FAKE_QUADLET_DIR}/${service}.container" "${dir}/unit.container"
    fi
    if [[ "${action}" == prepare ]]; then
      printf 'pair=previous\n' >"${dir}/50-worker-profile.conf"
      mkdir -p "${unit_dropin%/*}"
      cp "${dir}/50-worker-profile.conf" "${unit_dropin}"
    fi
    ;;
  rollback)
    [[ -f "${dir}/profile.json" ]] || exit 3
    [[ "$4" == "${dir%/*}/rollback.conf" ]] || exit 3
    printf 'pair=previous\napp_version=captured\n' >"$4"
    ;;
  candidate)
    expected="${FAKE_CANDIDATE_ID}"
    if [[ "${FAKE_COORDINATED:-false}" == true ]]; then
      expected="${FAKE_WORKER_ID}"
      [[ "${service}" != hololive-api ]] || expected="${FAKE_API_ID}"
    fi
    [[ "$4" == "${expected}" ]] || exit 3
    grep -q '^\[Container\]' || exit 3
    mkdir "${dir}"
    printf 'profile\n' >"${dir}/profile.json"
    printf 'pair=candidate\n' >"${dir}/50-worker-profile.conf"
    ;;
  check)
    [[ -f "${dir}/profile.json" ]] || exit 3
    [[ "${FAKE_CASE}" != candidate_check_failure || "${dir##*/}" != candidate ]]
    ;;
  verify|verify-source) [[ -f "${dir}/profile.json" ]] ;;
  verify-dropin)
    [[ -f "$4" && "$(<"$4")" == $'pair=previous\napp_version=captured' ]] ;;
  verify-captured)
    [[ "$4" == "${service}" && "$(<"${current}/runtime")" == baseline && "$(<"${current}/active")" == true &&
      "${FAKE_CASE}" != generation_changed ]] ;;
  verify-retained)
    [[ -f "${dir}/profile.json" && -f "$4/profile.json" && "$5" == "${FAKE_DEPLOY_ROOT}" ]] || exit 3
    if grep -qx 'pair=previous' "${unit_dropin}"; then
      cmp -s "$dir/50-worker-profile.conf" "$unit_dropin" || exit 3
    fi
    retained="$(sed -n 's/^pair=//p' "${unit_dropin}")"
    [[ "${retained}" == baseline || "${retained}" == previous || "${retained}" == candidate ]]
    ;;
  activate)
    [[ "$4" == "${FAKE_DEPLOY_ROOT}" ]] || exit 3
    [[ "${FAKE_CASE}" != api_activation_failure || "${service}" != hololive-api ]] || exit 1
    mkdir -p "${unit_dropin%/*}"
    if [[ "${dir##*/}" == previous ]]; then
      [[ "$#" == 5 && "$(<"$5")" == $'pair=previous\napp_version=captured' ]] || exit 3
      if [[ "$5" != "${dir}/50-worker-profile.conf" ]]; then cp "$5" "${dir}/50-worker-profile.conf"; fi
    fi
    cp "${dir}/50-worker-profile.conf" "${unit_dropin}"
    ;;
  verify-live)
    [[ "${FAKE_CASE}" != "verify_failure_${service}" ]] || exit 1
    [[ "${FAKE_CASE}" != failed_rehold || "$service" != hololive-alarm-worker ]] || exit 1
    [[ "$FAKE_CASE" != rehold_evidence_unwritable || "$service" != hololive-alarm-worker ]] || exit 1
    if [[ "$service" == hololive-api && "$FAKE_CASE" == last_pg_restart ]]; then printf '1794960722000000000\n' >"$state/pg-started"; fi
    expected="${dir##*/}"
    if [[ "${FAKE_COORDINATED:-false}" == true && "${expected}" == previous ]]; then expected=baseline; fi
    [[ "$4" == "${service}" && "$(<"${current}/active")" == true &&
      "$(<"${current}/runtime")" == "${expected}" ]] || exit 1
    if [[ "$FAKE_CASE" == worker_cid_changed && "$service" == hololive-alarm-worker ]]; then printf '%064d\n' 9 >"$current/cid"; fi
    ;;
  *) exit 2 ;;
esac
`,
}
