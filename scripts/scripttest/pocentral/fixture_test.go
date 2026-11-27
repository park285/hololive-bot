package pocentral

const proofSetup = `
set -euo pipefail
fixture_root="$1" fixture_case="$2" staging="$3" collector_container=collector
. "$staging/scripts/deploy/lib/postgres-endpoint.sh"
awk() {
  if [[ "${@: -1}" == /etc/stack-secrets/hololive-bot/compose.env ]]; then
    printf 'hololive_migrator\n'
  else
    command awk "$@"
  fi
}
timeout() { [[ "$1" == 20 ]] || return 2; shift; "$@"; }
podman() {
  case "$1" in
    inspect)
      if [[ "$3" == *'.Config.Env'* ]]; then
        cat "$fixture_root/collector.env"
      elif [[ "$4" == holo-postgres ]]; then
        printf '%064d\n' 0
      else
        return 2
      fi ;;
    run)
      printf '%s\n' "$@" > "$fixture_root/query-args"
      case "$fixture_case" in
        query_failure) return 7 ;;
        writable_proof) printf 'off|t\n' ;;
        wrong_checksum) printf 'on|wrong\n' ;;
        *) printf 'on|t\n' ;;
      esac ;;
    ps) return 0 ;;
    *) return 2 ;;
  esac
}
`

const transferEntry = `
set -eu
state="$1"
hostname() { printf 'kapu\n'; }
git() {
  [[ "$1" == -C && "$2" == */tool ]] || return 2
  case "$3" in
    status) ;;
    rev-parse) printf '%040d\n' 0 | tr 0 b ;;
    *) return 2 ;;
  esac
}
sudo() { [[ "$1" != -n ]] || shift; "$@"; }
ssh() {
  local command="${*:2}"
  command="${command//"$REMOTE_ROOT"/"$FAKE_REMOTE"}"
  case "$command" in
    'sudo -n mkdir '*) eval "${command#sudo -n }" ;;
    'sudo -n chown '*) ;;
    'sudo -n bash -s -- c quiesce-verified '*) cat > "$state/remote-body" ;;
    *) return 2 ;;
  esac
}
export -f hostname git sudo ssh
export state REMOTE_ROOT=/opt/hololive-bot/compose FAKE_REMOTE="$state/remote" PATH="$state/bin:$PATH" PO_C_SSH_TARGET=hololive-seoul APPROVE_PO_C_DEPLOY=true
bash "$state/tool/scripts/deploy/po-central-cutover.sh" c quiesce-verified "$state/pair" "$3"
`

const stagedEntry = `
set -eu
state="$1"
hostname() { printf 'kapu\n'; }
git() {
  local repo="$PWD"
  if [[ "$1" == -C ]]; then repo="$2"; shift 2; fi
  case "$repo" in "$state/coordinator"*) repo=coordinator ;; "$state/candidate"*) repo=candidate ;; *) return 2 ;; esac
  local rev
  if [[ "$repo" == coordinator ]]; then rev="$(printf '%040d' 1)"; else rev="$(printf '%040d' 0 | tr 0 b)"; fi
  case "$1" in
    status) [[ ! -f "$state/dirty" ]] || printf ' M caller\n' ;;
    rev-parse)
      if [[ "$2" == --show-toplevel ]]; then printf '%s/%s\n' "$state" "$repo"; else printf '%s\n' "$rev"; fi ;;
    cat-file)
      [[ "$2" == blob && "$3" == "$rev:"* ]] || return 2
      cat -- "$state/objects/$repo/${3#*:}" ;;
    *) return 2 ;;
  esac
}
ssh() {
  printf '%s\n' "$*" >> "$state/calls"
  cmp -s - <(bash -c '. "$1/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble b; cat -- "$1/scripts/deploy/lib/po-central-remote.sh"' _ "$state/objects/coordinator") || return 2
  [[ ! -f "$state/lost-reply" ]] || return 255
}
rsync() { printf 'unexpected rsync\n' >> "$state/calls"; return 7; }
podman() { printf 'unexpected build\n' >> "$state/calls"; return 7; }
export -f hostname git ssh rsync podman
export state PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_DEPLOY="${deploy_approval:-true}" APPROVE_PO_B_ROLLBACK="${rollback_approval:-false}"
resume_grant_args=()
if [[ "$2" == preflight || "$2" == quiesce ]]; then resume_grant_args=("${resume_grant_arg:-$state/prepared-resume-grant}"); fi
bash "${tool_root:-$state/coordinator}/scripts/deploy/po-central-cutover.sh" b "$2" "${product_root:-$state/candidate}" "${receipt_arg:-$state}" "${stage_id:-20261003T000000Z}" \
  "${window_id:-11111111111111111111111111111111}" "$state/w2" "${resume_grant_args[@]}"
`

const centralSetup = `
set -Eeuo pipefail
fixture_root="$1" fixture_case="$2"
if [[ "$slot" == b && "$fixture_case" == maintenance_quiesce ]]; then
  unset EPOCHREALTIME
  EPOCHREALTIME=1794975991.018375
  date() {
    if [[ "$*" == '-u +%Y-%m-%d %H:%M:%S.%6N UTC' ]]; then
      printf '2026-11-18 04:26:31.183751 UTC\n'
    else
      command date "$@"
    fi
  }
fi
compose_env="$fixture_root/compose.env"
remote_root="$fixture_root"
current="$fixture_root/current" staging="$fixture_root/staging" backup="$fixture_root/card-po-$slot-20261003T000000Z"
unit_dir="$fixture_root/units"
revision=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb version=6.0.1
pair_tool="$staging/scripts/deploy/lib/worker-profile-pair.py"
cutover_started=false
mode=deploy
[[ "$fixture_case" != check_* ]] || mode=check
[[ "$fixture_case" != rollback* ]] || mode=rollback
case "$fixture_case" in
  maintenance_quiesce*) mode=quiesce ;;
  maintenance_resume*) mode=resume ;;
  maintenance_check*) mode=check ;;
  maintenance_rollback*) mode=rollback ;;
esac
transport=-
if [[ -f "$fixture_root/staged-pair" ]]; then
  if [[ "$mode" != deploy ]]; then backup="$fixture_root/staged-po-b-20261003T000000Z"; fi
  if [[ "$mode" != deploy ]]; then transport="$(<"$fixture_root/trusted-seal")"; fi
elif [[ "$mode" == quiesce && -f "$fixture_root/trusted-seal" ]]; then
  mode=quiesce-verified transport="$(<"$fixture_root/trusted-seal")"
fi
set -- "$slot" "$mode" "$staging" "$backup" "$revision" "$version" "$transport"
if [[ "$backup" == "$fixture_root/staged-po-b-"* ]]; then
  read -r -a origin_fields < "$fixture_root/approved-origin"
  set -- "$@" "${origin_fields[@]}"
fi
printf -v fixture_zeros '%064d' 0
fixture_hex_old="${fixture_zeros//0/a}" fixture_hex_new="${fixture_zeros//0/b}"
fixture_revision_old="${fixture_hex_old:0:40}" fixture_revision_new="${fixture_hex_new:0:40}"
hex_of() { if [[ "$1" == old ]]; then printf '%s\n' "$fixture_hex_old"; else printf '%s\n' "$fixture_hex_new"; fi; }
revision_of() { if [[ "$1" == old ]]; then printf '%s\n' "$fixture_revision_old"; else printf '%s\n' "$fixture_revision_new"; fi; }
fixture_exit_status() {
  local unit="$1" cid="$2" since="$3" until="$EPOCHREALTIME" id at code found=''
  [[ "$since" =~ ^@[0-9]+\.[0-9]{6}$ && "$cid" =~ ^[0-9a-f]{64}$ &&
     -f "$fixture_root/$unit.died" && "$(<"$fixture_root/$unit.since")" == "$since" ]] || return 8
  if [[ "$slot" == b && "$fixture_case" == maintenance_quiesce ]]; then until=1794975991.020000; fi
  while read -r id at code; do
    [[ "$id" == "$cid" && ! "$at" < "${since#@}" && ! "$at" > "$until" ]] || continue
    [[ -z "$found" && "$code" =~ ^(0|[1-9][0-9]{0,2})$ && "$code" -le 255 ]] || return 8
    found="$code"
  done < "$fixture_root/$unit.died"
  [[ -n "$found" ]] && printf '%s\n' "$found"
}
sleep() {
  if [[ "$fixture_case" != *late_notify_client || "$1" != 0.25 || ! -e "$fixture_root/notify.pending" ]]; then
    command sleep "$@"; return $?
  fi
  if [[ ! -e "$fixture_root/notify.246s" ]]; then
    SECONDS=$((SECONDS + 246))
    : > "$fixture_root/notify.246s"
  else
    rm -- "$fixture_root/notify.pending"
    printf true > "$fixture_root/collector.running"
  fi
}
guard_cli() {
  local op="$1" unit="$2" anchor="$3" inode="$4" mask="$system_unit_dir/$2" guard="$system_unit_dir/$2.guard"
  printf 'guard %s %s\n' "$op" "$unit" >> "$fixture_root/calls"
  if [[ "$op" == exit-status ]]; then fixture_exit_status "$unit" "$3" "$4" || return 8; return 0; fi
  [[ "$(readlink "$anchor")" == /dev/null && "$(command stat -c '%d:%i' "$anchor")" == "$inode" ]] || return 8
  if [[ "$op" == arm && "$fixture_case" == maintenance_quiesce_mask_race && "$unit" == "$collector_unit" ]]; then
    command ln -s /dev/null "$mask" || return 8
  fi
  [[ ! -e "$mask" && ! -L "$mask" ]] || return 8
  case "$op" in
    arm)
      (set -C; printf '%s\n' "$anchor" > "$guard") || return 8
      [[ "$(systemctl show -p LoadState --value "$unit")" == loaded ]] || return 8
      systemctl daemon-reload || return 8
      if [[ "$unit" == "$collector_unit" && "$fixture_case" == *arm_collector_profile ]]; then
        printf 'other profile\n' > "$fixture_root/loaded.profile"
      fi
      [[ "$unit" != "$collector_unit" || "$fixture_case" != *guard_failure ]] ;;
    undo-unmask)
      [[ "$(systemctl show -p LoadState --value "$unit")" == masked && ! -e "$guard" ]] || return 8
      ln -PT -- "$anchor" "$mask" && systemctl daemon-reload || return 8 ;;
    start-budget)
      [[ -f "$guard" && "$(<"$guard")" == "$anchor" ]] || return 8
      if [[ "$unit" == "$collector_unit" ]]; then printf '300\n'; else printf '120\n'; fi ;;
    check|seal)
      [[ -f "$guard" && "$(<"$guard")" == "$anchor" ]] || return 8
      [[ "$unit" != "$collector_unit" || "$fixture_case" != *guard_check_failure ]] || return 8
      if [[ "$op" == seal ]]; then
        [[ "$(systemctl show -p ActiveState --value "$unit")" =~ ^(inactive|failed)$ &&
           "$(systemctl show -p MainPID --value "$unit")" == 0 && "$(systemctl show -p ControlPID --value "$unit")" == 0 ]] || return 8
        ln -PT -- "$anchor" "$mask" && systemctl daemon-reload && rm -- "$guard" || return 8
      fi ;;
    *) return 2 ;;
  esac
}
admitted() {
  local mask="$system_unit_dir/$1" guard="$system_unit_dir/$1.guard"
  [[ ! -L "$mask" ]] && [[ ! -f "$guard" || ! -e "$(<"$guard")" ]]
}
python3() {
  if [[ "${1-} ${2-} ${3-}" == "-I -B $staging/scripts/deploy/lib/systemd-stop-guard.py" ]]; then
    shift 3; guard_cli "$@"
  elif [[ "${1:-}" == "$pair_tool" ]]; then
    printf 'pair %s %s\n' "$2" "$4" >>"$fixture_root/calls"
    case "$2" in
      capture)
        mkdir -p "$4"; printf 'retained profile\n' > "$4/profile.json"
        cp "$fixture_root/collector.generation" "$4/pair.json"
        cp "$unit_dir/$collector_container.container" "$4/unit.container" ;;
      candidate)
        mkdir -p "$4"
        cat >"$4/unit.container"
        [[ "$5" == "sha256:$fixture_hex_new" ]] || return 3
        printf 'Image=%s\n' "$5" > "$4/50-worker-profile.conf"
        if [[ -f "$fixture_root/source.profile" ]]; then
          cp "$fixture_root/source.profile" "$4/profile.json"
        else
          printf 'candidate profile\n' > "$4/profile.json"
        fi ;;
      rollback) test -f "$4/profile.json"; touch "$4/50-worker-profile.conf" ;;
      check)
        if [[ "$fixture_case" == *candidate_check_failure && "$4" == */candidate ]]; then return 7; fi
        test -f "$4/profile.json" ;;
      verify) test -f "$4/profile.json" ;;
      verify-live)
        if [[ "$fixture_case" == *rollback_replaced && "$4" == */previous ]]; then
          printf 9 > "$fixture_root/collector.cid"; printf '%032x\n' 3 > "$fixture_root/collector.invocation"; return 7
        fi
        [[ "$5" == "$collector_container" ]] && cmp -s "$4/profile.json" "$fixture_root/loaded.profile" ;;
      verify-captured)
        [[ "$5" == "$collector_container" ]] && cmp -s "$4/pair.json" "$fixture_root/collector.generation" &&
          cmp -s "$4/profile.json" "$fixture_root/loaded.profile" ;;
      check-database)
        [[ "$fixture_case" != *database_query_failure && "$3" == "$service" && "$4" == "$profile_release/candidate" ]] &&
          cmp -s "$4/unit.container" "$unit_dir/$collector_container.container" ;;
      verify-dropin)
        cmp "$4/50-worker-profile.conf" <(printf 'Image=sha256:%s\n' "$fixture_hex_new") ;;
      verify-source)
        test -f "$4/profile.json"
        if [[ -f "$fixture_root/source.profile" ]]; then cmp "$fixture_root/source.profile" "$4/profile.json"; fi ;;
      activate)
        [[ "$fixture_case" != *activation_failure ]] || return 7
        test -f "$4/profile.json"
        basename "$4" > "$fixture_root/dropin" ;;
      *) return 2 ;;
    esac
  else
    command python3 "$@"
  fi
}
busctl() {
  local bus=org.freedesktop.systemd1
  [[ "$*" == "--system --timeout=45s --json=short get-property $bus $6 $bus.Service TimeoutStartUSec" ]] || return 2
  case "$6" in
    "$(systemd_unit_object "$collector_unit")") printf '{"type":"t","data":300000000}\n' ;;
    "$(systemd_unit_object "$issuer_unit")") printf '{"type":"t","data":120000000}\n' ;;
    *) return 2 ;;
  esac
}
sudo() { [[ "$1" != -n ]] || shift; "$@"; }
install() {
  local args=()
  while (($#)); do
    case "$1" in -o|-g) shift 2 ;; *) args+=("$1"); shift ;; esac
  done
  command install "${args[@]}"
}
rm() {
  command rm "$@" || return
  if [[ "$fixture_case" == *receipt_signal* && "$*" == "-- $hold/quiesced" ]]; then
    if [[ "$fixture_case" == *receipt_signal_changed ]]; then printf '137\n' > "$fixture_root/issuer.exit"; fi
    kill -TERM "$maintenance_owner_pid"
  fi
}
sync() {
  if [[ "$fixture_case" == *error_log_evidence_failure && "$2" == "$backup/guarded-stop.log" ]]; then return 8; fi
  if [[ "$fixture_case" == maintenance_resume_expired_late_notify_client &&
        "$2" == "$backup/resume-$collector_unit.start-request" && -f "$fixture_root/notify.pending" ]]; then
    unset EPOCHSECONDS; EPOCHSECONDS=$expires
  fi
  command sync "$@"
}
ln() {
  if [[ "${*: -1}" == "$system_unit_dir/"* ]]; then printf 'mask link %s\n' "${*: -1}" >>"$fixture_root/calls"; fi
  if [[ "$1" == -PT && "$4" == "$system_unit_dir/$collector_unit" ]]; then
    [[ "$fixture_case" != maintenance_quiesce_mask_failure && "$fixture_case" != *error_log_seal_failure ]] || return 8
  fi
  command ln "$@"
}
stat() {
  case "$2:${*: -1}" in
    '%d:%i:'*) command stat "$@" ;;
    '%u:%g:'*) printf '0:0\n' ;;
    *:"$fixture_root/socket") printf '65532:1000 770\n' ;;
    *) printf '0:0 %s\n' "$(command stat -c '%a' "${*: -1}")" ;;
  esac
}
find() {
  local arg
  local -a args=()
  while (( $# )); do
    arg="$1"; shift; args+=("$arg")
    case "$arg" in
      -uid) [[ "$1" == 0 ]] || return 2; args+=("$EUID"); shift ;;
      -gid) [[ "$1" == 0 ]] || return 2; args+=("$(id -g)"); shift ;;
    esac
  done
  command find "${args[@]}"
}
date() {
  if [[ "$*" == '-u +%Y-%m-%dT%H:%M:%SZ' ]]; then printf '2026-10-03T00:01:00Z\n'; else command date "$@"; fi
}
require_collector_lease_migration() {
  printf 'lease-proof %s\n' "$1" >>"$fixture_root/calls"
  [[ "$1" != target || ! -e "$fixture_root/pending-suffix" ]]
}
podman() {
  local object kind value format file
  printf 'podman %s\n' "$*" >>"$fixture_root/calls"
  case "$1 $2" in
    'image inspect')
      file="${*: -1}" format="$4"
      file="$fixture_root/images/${file//[\/:]/_}"
      [[ -f "$file" ]] || return 125
      value="$(<"$file")"
      case "$format" in
        '{{.Id}}') hex_of "$value" ;;
        '{{index .Config.Labels "org.opencontainers.image.revision"}} {{.Os}}/{{.Architecture}} {{.Id}}') printf '%s linux/arm64 %s\n' "$(revision_of "$value")" "$(hex_of "$value")" ;;
        *image.revision*) revision_of "$value" ;;
        *image.version*) if [[ "$value" == old ]]; then printf '6.0.0\n'; else printf '6.0.1\n'; fi ;;
        '{{.Os}}/{{.Architecture}}') printf 'linux/arm64\n' ;;
        *) return 2 ;;
      esac
      ;;
    'image exists')
      [[ "$fixture_case" != inventory_failure ]] || return 125
      [[ -f "$fixture_root/images/${3//[\/:]/_}" ]]
      ;;
    'container exists')
      [[ ! -f "$fixture_root/inventory-unavailable" ]] || return 125
      if [[ -f "$fixture_root/staged-pair" ]]; then
        local cid
        for kind in collector issuer; do
          cid=3; [[ "$kind" != issuer ]] || cid=4
          if [[ -f "$fixture_root/$kind.cid" ]]; then cid="$(<"$fixture_root/$kind.cid")"; fi
          local name="$collector_container"
          [[ "$kind" != issuer ]] || name="$issuer_container"
          printf -v cid '%064d' "$cid"
          if [[ "$3" == "$name" || "$3" == "$cid" ]]; then
            [[ "$(<"$fixture_root/$kind.running")" == true ]]; return
          fi
        done
        return 1
      fi
      local collector_cid issuer_cid
      printf -v collector_cid '%064d' 3
      printf -v issuer_cid '%064d' 4
      if [[ "$fixture_case" == *stop_leftover_id && "$3" == "$collector_cid" ]]; then return 0; fi
      if [[ "$3" == "$issuer_cid" && -e "$fixture_root/issuer.old-cid" ]]; then return 0; fi
      kind=collector
      [[ "$3" != "$issuer_container" && "$3" != "$issuer_cid" ]] || kind=issuer
      [[ "$(<"$fixture_root/$kind.running")" == true || -e "$fixture_root/$kind.leftover" ]]
      ;;
    'image rm') rm "$fixture_root/images/${3//[\/:]/_}" ;;
    'tag '*)
      if [[ "$fixture_case" == restore_failure && "$2" == *:rollback-* ]]; then return 9; fi
      cp "$fixture_root/images/${2//[\/:]/_}" "$fixture_root/images/${3//[\/:]/_}"
      ;;
    'load --input')
      kind=po-sandbox
      [[ "$3" != */collector-image.tar ]] || kind=collector
      local image="$issuer_candidate"
      [[ "$kind" != collector ]] || image="$collector_candidate"
      printf new > "$fixture_root/images/${image//[\/:]/_}"
      if [[ "$kind" == collector ]]; then
        case "$fixture_case" in
          *load_collector_replaced) printf '{"container_id":"%064d","pid":42}\n' 2 > "$fixture_root/collector.generation" ;;
          *load_collector_restarted) printf '{"container_id":"%064d","pid":43}\n' 1 > "$fixture_root/collector.generation" ;;
          *load_issuer_invocation) printf '%032x\n' 2 > "$fixture_root/issuer.invocation" ;;
          *load_issuer_image) printf new > "$fixture_root/issuer.runtime"; printf new > "$fixture_root/images/${issuer//[\/:]/_}" ;;
          *load_issuer_restarted) printf 2 > "$fixture_root/issuer.started" ;;
          *load_issuer_unhealthy) touch "$fixture_root/issuer.unhealthy" ;;
          *load_issuer_socket) touch "$fixture_root/issuer.socket-failed" ;;
        esac
      fi
      ;;
    'inspect -f')
      object="${*: -1}" kind=collector format="$3"
      [[ "$object" != "$issuer_container" ]] || kind=issuer
      [[ "$(<"$fixture_root/$kind.running")" == true ]] || return 125
      value="$(<"$fixture_root/$kind.runtime")"
      case "$format" in
        *Config.Env*)
          if [[ "$slot" == c ]]; then
            if [[ -f "$fixture_root/direct-origin" && "$value" == old || "$fixture_case" == transition_* && "$value" == old || "$fixture_case" == transition_wrong_endpoint ]]; then
              printf 'POSTGRES_HOST=other-pooler\nPOSTGRES_PORT=6000\nPOSTGRES_DB=hololive\n'
            else
              printf 'POSTGRES_HOST=holo-postgres\nPOSTGRES_PORT=5432\nPOSTGRES_DB=hololive\n'
            fi
          else
            if [[ -f "$fixture_root/direct-origin" && "$value" == old || "$fixture_case" == transition_* && "$value" == old || "$fixture_case" == transition_wrong_endpoint ]]; then
              printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=6001\nPOSTGRES_DB=hololive\n'
            else
              printf 'POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=5433\nPOSTGRES_DB=hololive\n'
            fi
          fi ;;
        *State.Health*)
          if [[ -e "$fixture_root/$kind.unhealthy" ]]; then printf 'unhealthy\n'; else printf 'healthy\n'; fi ;;
        *State.StartedAt.UnixNano*)
          local cid=3 conmon=42; [[ "$kind" != issuer ]] || cid=4
          if [[ -f "$fixture_root/$kind.cid" ]]; then cid="$(<"$fixture_root/$kind.cid")"; fi
          if [[ -f "$fixture_root/$kind.foreign-conmon" ]]; then conmon=99; fi
          local hex="$fixture_hex_new"; [[ "$value" != old ]] || hex="$fixture_hex_old"
          printf '%064d %s %s 43 %d\n' "$cid" "$hex" "$(<"$fixture_root/$kind.started")" "$conmon"
          if [[ "$fixture_case" == *capture_cid_changed && "$kind" == issuer ]]; then printf 9 > "$fixture_root/$kind.cid"; fi ;;
        *image.revision*) revision_of "$value" ;;
        '{{.Image}}') hex_of "$value" ;;
        *Mounts*) printf '%s/socket\n' "$fixture_root" ;;
        '{{.State.StartedAt.Unix}}') command date -u -d 2026-10-03T00:01:01Z +%s ;;
        *) return 2 ;;
      esac
      ;;
    'exec '*)
      if [[ "$2" == "$issuer_container" ]]; then
        [[ ! -e "$fixture_root/issuer.socket-failed" ]] || return 1
        if [[ "$fixture_case" == maintenance_resume_expire_after_issuer ]]; then unset EPOCHSECONDS; EPOCHSECONDS=$expires; fi
      else
        [[ "$2" == "$collector_container" && "$4" == --body ]] || return 2
        local instance="collector-$slot" handoff=PROCESSED processed=true
        if [[ "$fixture_case" == check_wrong_instance ]]; then instance=collector-a; fi
        if [[ "$fixture_case" == check_waiting_handoff ]]; then handoff=PENDING; processed=false; fi
        printf '{"status":"ready","instance_id":"%s","helper":"ok","first_success":true,"handoff_status":"%s","handoff_processed":%s,"pending_queue":0}\n' \
          "$instance" "$handoff" "$processed"
      fi ;;
    'logs --since')
      case "$fixture_case:${*: -1}" in
        *logs_collector*:"$collector_container") return 5 ;;
        *error_log_replaced:"$collector_container") printf 9 > "$fixture_root/collector.cid"; printf 'ERR replaced\n' ;;
        *error_log*:"$collector_container") printf 'ERR injected runtime error\n' ;;
        *) printf 'runtime ready\n' ;;
      esac
      ;;
    *) return 2 ;;
  esac
}
journalctl() {
  if [[ "${11-}" == JOB_TYPE=start ]]; then
    local unit="${9#UNIT=}" id invocation
    [[ "$8" == _PID=1 && "${12}" == JOB_RESULT=done && "${13}" == MESSAGE_ID=39f53479d3a045ac8e11786248231fbf ]] || return 2
    read -r id invocation < "$fixture_root/$unit.start-job" || return 8
    [[ "${10}" == "JOB_ID=$id" ]] || return 8
    printf '{"INVOCATION_ID":"%s"}\n' "$invocation"
    return 0
  fi
  local unit="${11#UNIT=}" invocation="${12#INVOCATION_ID=}" since="${9#--since=}" id at
  [[ "${10}" == _PID=1 ]] || return 2
  printf '%s\n' "$since" > "$fixture_root/$unit.since"
  since="$(command date -u -d "$since" +%s.%N)" || return 2
  since="${since%???}"
  [[ "$unit" == "$issuer_unit" || "$unit" == "$collector_unit" ]] || return 2
  [[ "${13}" == JOB_TYPE=stop && "${14}" == MESSAGE_ID=de5b426a63be47a7b6ac3eaac82e2f6f ]] || return 2
  if [[ -f "$fixture_root/$unit.stopping" ]] && read -r id at < "$fixture_root/$unit.stopping" &&
     [[ "$id" == "$invocation" && ! "$at" < "$since" ]]; then
    printf '{"INVOCATION_ID":"%s"}\n' "$id"
  fi
}
systemctl() {
  local kind=collector value
  printf 'systemctl %s\n' "$*" >>"$fixture_root/calls"
  [[ "${*: -1}" != "$issuer_unit" ]] || kind=issuer
  if [[ "$1" == show && "$2" == --all ]]; then
    local prop answer unit="${*: -1}"
    shift 2
    while [[ "$1" == -p ]]; do
      prop="$2"; shift 2
      answer="$(systemctl show -p "$prop" --value "$unit")" || return
      printf '%s=%s\n' "$prop" "$answer"
    done
    return 0
  fi
  case "$1" in
    start)
      admitted "$2" || return 8
      printf true > "$fixture_root/$kind.running" ;;
    daemon-reload)
      if [[ "$fixture_case" == *collector_143 && -L "$system_unit_dir/$collector_unit" ]]; then
        rm -f "$fixture_root/collector.exit"
      fi ;;
    show)
      if [[ "$2" == --property=Job && "$3" == --property=ActiveState ]]; then
        [[ "$fixture_case" != manager_probe_failure || "$cutover_started" != true ]] || return 8
        if [[ -f "$fixture_root/manager-job" ]] && [[ "$(<"$fixture_root/manager-job")" == "${*: -1}" ]]; then
          printf 'Job=17\nActiveState=activating\n'
        elif [[ "$(<"$fixture_root/$kind.running")" == true ]]; then
          printf 'Job=\nActiveState=active\n'
        else
          printf 'Job=\nActiveState=failed\n'
        fi
        return 0
      fi
      case "$3" in
        Job)
          [[ ! -e "$fixture_root/$kind.probe-failed" ]] || return 8
          if [[ "$kind" == collector && -e "$fixture_root/notify.pending" ]]; then printf '18\n'; else printf '\n'; fi ;;
        NRestarts)
          if [[ "$fixture_case" == *capture_restarted && "$kind" == issuer ]]; then printf '1\n'; else printf '0\n'; fi ;;
        LoadState)
          if [[ -L "$system_unit_dir/${*: -1}" || -e "$fixture_root/$kind.masked-cache" ]]; then printf 'masked\n'; else printf 'loaded\n'; fi ;;
        ActiveState)
          if [[ "$kind" == collector && -e "$fixture_root/notify.pending" ]]; then printf 'activating\n'; return 0; fi
          if [[ "$(<"$fixture_root/$kind.running")" == true ]]; then printf 'active\n'
          elif [[ -f "$fixture_root/$kind.exit" ]]; then printf 'failed\n'; else printf 'inactive\n'; fi ;;
        InvocationID)
          cat "$fixture_root/$kind.invocation" ;;
        Result) if [[ -f "$fixture_root/$kind.exit" ]]; then printf 'exit-code\n'; else printf 'success\n'; fi ;;
        ExecMainCode) if [[ -s "$fixture_root/$kind.invocation" ]]; then printf '1\n'; else printf '0\n'; fi ;;
        ExecMainStatus) if [[ -f "$fixture_root/$kind.exit" ]]; then cat "$fixture_root/$kind.exit"; else printf '0\n'; fi ;;
        MainPID|ControlPID)
          if [[ "$3" == ControlPID && "$fixture_case" == *stop_control_pid && "$kind" == collector ]]; then printf '42\n'
          elif [[ "$3" == MainPID && "$(<"$fixture_root/$kind.running")" == true ]]; then printf '42\n'
          else printf '0\n'; fi ;;
        *) return 2 ;;
      esac ;;
    unmask)
      [[ "$fixture_case" != *unmask_failure || "$kind" != collector ]] || return 8
      if [[ "$fixture_case" == *peer_probe_failure && "$kind" == collector ]]; then
        touch "$fixture_root/$kind.probe-failed"; return 8
      fi
      rm "$system_unit_dir/$2" || return 8
      if [[ "$fixture_case" == maintenance_resume_expire_after_unmask && "$kind" == issuer ]]; then unset EPOCHSECONDS; EPOCHSECONDS=$expires; fi
      if [[ "$fixture_case" == *unmask_reload* && "$kind" == issuer ]]; then
        touch "$fixture_root/$kind.masked-cache"
        if [[ "$fixture_case" == *unmask_reload_old_cid ]]; then touch "$fixture_root/issuer.old-cid"; fi
        return 8
      fi ;;
    is-active) [[ "$(<"$fixture_root/$kind.running")" == true ]] ;;

    restart|--show-transaction)
      if [[ "$1" == --show-transaction ]]; then
        shift; [[ "$1" == start ]] || return 2
        local job=17; [[ "$kind" == issuer ]] || job=18
        printf 'Enqueued anchor job %s %s/start.\n' "$job" "$2" >&2
      fi
      admitted "$2" || return 8
      rm -f "$fixture_root/$kind.exit"
      printf '%032x\n' 2 > "$fixture_root/$kind.invocation"
      if [[ -f "$fixture_root/staged-pair" ]]; then
        local cid=5 invocation=2
        if [[ "$mode" == rollback ]]; then cid=7 invocation=3; fi
        [[ "$kind" != issuer ]] || cid=$((cid+1))
        if [[ "$fixture_case" == *old_start_cid && "$kind" == collector ]]; then cid=3; fi
        printf '%d' "$cid" > "$fixture_root/$kind.cid"
        printf '%032x\n' "$invocation" > "$fixture_root/$kind.invocation"
      fi
      if [[ "$fixture_case" == maintenance_* ]]; then
        [[ ! -e "$fixture_root/db.stopped" ]] || return 9
        test -f "$profile_release/candidate/profile.json" || [[ "$mode" == rollback ]] || return 9
        test -f "$current/$manifest" && test -f "$current/$image_id_file" || return 9
        if [[ "$mode" == resume ]]; then
          [[ "$(<"$current/$image_id_file")" == "sha256:$fixture_hex_new" ]] || return 9
        fi
        [[ "$(<"$fixture_root/dropin")" == candidate || "$mode" == rollback ]] || return 9
      fi
      if [[ "$fixture_case" == client_lost_pending && "$kind" == issuer && ! -f "$fixture_root/manager-job" ]]; then
        local client_status=0
        command python3 "$fixture_root/manager-client.py" "$fixture_root/manager.sock" "$issuer_unit" || client_status=$?
        return "$client_status"
      fi
      if [[ "$kind" == collector ]]; then
        case "$(<"$fixture_root/dropin")" in
          candidate) value=new ;;
          previous) value=old ;;
          *) value="$(<"$fixture_root/images/${collector//[\/:]/_}")" ;;
        esac
        cp "$profile_release/$(<"$fixture_root/dropin")/profile.json" "$fixture_root/loaded.profile"
        if [[ "$fixture_case" == *rollback_profile ]]; then printf 'other profile\n' > "$fixture_root/loaded.profile"; fi
        if [[ "$fixture_case" == *rollback_unhealthy ]]; then touch "$fixture_root/collector.unhealthy"; fi
        if [[ "$fixture_case" == *rollback_image ]]; then value=new; fi
      else
        value="$(<"$fixture_root/images/${issuer//[\/:]/_}")"
      fi
      printf '%s' "$value" > "$fixture_root/$kind.runtime"
      if [[ "$fixture_case" == *substitution_failure || "$fixture_case" == restore_failure || "$fixture_case" == manager_probe_failure ]] && [[ "$kind" == issuer && "$value" == new ]]; then
        printf false > "$fixture_root/$kind.running"
        printf '143\n' > "$fixture_root/issuer.exit"
        return 1
      fi
      printf true > "$fixture_root/$kind.running"
      if [[ "$fixture_case" == *rollback_issuer_failure && "$kind" == issuer ]]; then touch "$fixture_root/issuer.socket-failed"; fi
      if [[ "$1" == start ]]; then
        if [[ "$fixture_case" == *missing_start_record && "$kind" == collector ]]; then return 0; fi
        printf '%s %s\n' "$job" "$(<"$fixture_root/$kind.invocation")" > "$fixture_root/$2.start-job"
        if [[ "$kind" == collector && "$fixture_case" == *late_notify_client ]]; then
          : > "$fixture_root/notify.pending"; printf false > "$fixture_root/collector.running"; return 9
        fi
        if [[ "$kind" == issuer ]]; then
          case "$fixture_case" in
            *capture_inv_changed) printf '%032x\n' 3 > "$fixture_root/$kind.invocation" ;;
            *client_unknown) return 9 ;;
            *foreign_named_cid) printf 9 > "$fixture_root/$kind.cid"; : > "$fixture_root/$kind.foreign-conmon"; return 9 ;;
            maintenance_resume_signal_group_TERM) kill -s TERM -- "-$maintenance_owner_pid" ;;
            maintenance_resume_signal_*) kill -s "${fixture_case##*_}" "$maintenance_owner_pid" ;;
          esac
        fi
      fi
      if [[ "$fixture_case" == signal_* && "$kind" == collector && "$value" == new ]]; then
        kill -s "${fixture_case#signal_}" "$cutover_restore_owner_pid"
      fi
      ;;
    stop)
      if [[ "$fixture_case" == maintenance_* || "$fixture_case" == transition_* ]]; then
        [[ ! -L "$system_unit_dir/$2" && -f "$system_unit_dir/$2.guard" ]] || return 8
        if [[ "$mode" == quiesce ]] && { admitted "$issuer_unit" || admitted "$collector_unit"; }; then return 8; fi
      fi
      [[ "$fixture_case" != *stop_failure || "$kind" != collector ]] || return 8
      if [[ "$fixture_case" == *stop_peer_restarted && "$kind" == issuer ]]; then
        printf 2 > "$fixture_root/collector.started"
      fi
      if [[ "$fixture_case" == *stop_other_invocation && "$kind" == collector ]]; then
        printf '%032x\n' 2 > "$fixture_root/$kind.invocation"
      fi
      local at="$EPOCHREALTIME" cid=3 code=0
      if [[ "$slot" == b && "$fixture_case" == maintenance_quiesce ]]; then at=1794975991.019117; fi
      if [[ "$fixture_case" != *stop_missing_job || "$kind" != issuer ]]; then
        if [[ "$fixture_case" == *stop_stale_job && "$kind" == issuer ]]; then at=0; fi
        printf '%s %s\n' "$(<"$fixture_root/$kind.invocation")" "$at" > "$fixture_root/$2.stopping"
      fi
      if [[ "$kind" == issuer && "$mode" == quiesce ]]; then printf '143\n' > "$fixture_root/issuer.exit"; fi
      if [[ "$fixture_case" == *collector_143 && "$kind" == collector ]]; then printf '143\n' > "$fixture_root/collector.exit"; fi
      if [[ "$kind" == issuer ]]; then cid=4; fi
      if [[ -f "$fixture_root/staged-pair" && -f "$fixture_root/$kind.cid" ]]; then cid="$(<"$fixture_root/$kind.cid")"; fi
      if [[ -f "$fixture_root/$kind.exit" ]]; then code="$(<"$fixture_root/$kind.exit")"; fi
      if [[ "$kind" == collector ]]; then
        case "$fixture_case" in
          *died_stale) at=0 ;;
          *died_other_cid) cid=9 ;;
          *died_duplicate) printf '%064d %s %s\n' "$cid" "$at" "$code" >> "$fixture_root/$2.died" ;;
          *collected_143) code=143 ;;
        esac
      fi
      printf '%064d %s %s\n' "$cid" "$at" "$code" >> "$fixture_root/$2.died"
      if [[ "$fixture_case" == *stop_terminal_invocation && "$kind" == collector ]]; then
        printf '%032x\n' 2 > "$fixture_root/$kind.invocation"
      elif [[ ! -f "$fixture_root/$kind.exit" ]]; then
        : > "$fixture_root/$kind.invocation"
      fi
      printf absent > "$fixture_root/$kind.runtime"; printf false > "$fixture_root/$kind.running"
      if [[ "$kind" == issuer && "$fixture_case" == maintenance_quiesce_expire_after_stop ]]; then unset EPOCHSECONDS; EPOCHSECONDS=$expires; fi ;;
    *) return 2 ;;
  esac
}
if [[ "$fixture_case" == *grep_failure ]]; then grep() { if [[ "$*" == *'ERR|panic'* ]]; then return 2; else command grep "$@"; fi; }; fi
`
