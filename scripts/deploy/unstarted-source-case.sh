#!/usr/bin/env bash
set -euo pipefail

fail() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }
root=/opt/hololive-bot
compose_root=$root/compose
legacy=$compose_root/current
store=$compose_root/app-sources
sources=$root/release-sources
source=$(</tmp/source-revision)
writer=$(</tmp/accepted-revision)
candidate=$(</tmp/candidate-revision)
receipt=$store/transition-$source-$candidate
old_api=$sources/dv-holo-api-$source
old_tool=$sources/dv-tool-$source
next=$sources/candidate/source
tool=$sources/tool/source
stage=$sources/dv-holo-api-$candidate
worker_stage_new=$sources/dv-holo-worker-$candidate
tool_stage=$sources/dv-tool-$candidate
quad=/etc/containers/systemd/hololive-bot
migrate=hololive-db-migrate
api=hololive-api
worker=hololive-alarm-worker
overlay=$quad/$migrate.container.d/50-migrator-image.conf
old_image=$(printf 'sha256:%064d' 2)
mkdir /case/bin
for name in podman systemctl journalctl busctl; do ln -s "/usr/bin/$name" "/case/bin/$name"; done
for name in ip nft tailscale; do ln -s /case/unstarted-source-case-host.py "/case/bin/$name"; done
export PATH=/case/bin:/stubs:/usr/bin:/usr/sbin
printf '%s\n' "$$" >/tmp/infra-pid
: >/tmp/fault
until (( EPOCHSECONDS > $(stat -c %Z /etc/systemd) + 32 )); do sleep 1; done
cleanup() {
  if [[ -f /tmp/extra-pids ]]; then
    while read -r pid jail; do
      if [[ /proc/$pid/root -ef "$jail" ]]; then kill "$pid" 2>/dev/null || :; fi
    done </tmp/extra-pids
  fi
  if [[ -n "${ticker:-}" ]]; then kill "$ticker" 2>/dev/null || :; wait "$ticker" 2>/dev/null || :; fi
}
trap cleanup EXIT
chmod 0700 "$sources"
mkdir -p "${overlay%/*}"
printf '[Container]\nImage=%s\n' "$old_image" >"$overlay"
printf 'sha256:%064d\n' 6 >"${next%/source}/$api.candidate-image"
printf 'sha256:%064d\n' 7 >"${next%/source}/$worker.candidate-image"
secrets=/etc/stack-secrets/hololive-bot
printf '%s\n' LIVE_LOGS_PATH=/opt/hololive-bot/compose/current/logs HOLOLIVE_API_PORT_BIND_IP=127.0.0.1 \
  HOLOLIVE_ADMIN_API_PORT_BIND_IP=100.100.1.8 HOLOLIVE_H3_SERVER_NAME=localhost HOLOLIVE_DB_PASSWORD=fixture \
  HOLOLIVE_ALARM_WORKER_PASSWORD=fixture HOLOLIVE_MIGRATOR_PASSWORD=fixture >>"$secrets/compose.env"
mkdir -p "$secrets/certs"
for name in hololive-h3.crt hololive-h3.key iris-ca.pem postgres-ca.pem valkey-api-password valkey-discovery-password valkey-worker-password; do
  printf 'fixture\n' >"$secrets/certs/$name"
done
for name in bot alarm-worker; do printf 'FIXTURE=1\n' >"$secrets/$name.env"; done
. "$legacy/scripts/deploy/lib/postgres-capacity.sh"
postgres_capacity_write_quadlet_config "$legacy" "$secrets/compose.env" "$quad" /tmp/pooler-config
cat /tmp/pooler-config >/run/hololive-bot/pgbouncer/pgbouncer.ini
export SYNC_TEST_EVENTS=/events
umask 077
records() { python3 -ISB /case/unstarted-source-case-evidence.py "$@"; }
if [[ -f /tmp/scenario && $(</tmp/scenario) == missing-optional ]]; then
  sed -i 's/^HOLOLIVE_X_SPACES_ENABLED=0$/HOLOLIVE_X_SPACES_ENABLED=1/' "$secrets/compose.env"
  for name in "$api" "$worker"; do cp "$legacy/deploy/quadlet/optional/x-spaces/$name.container.d/40-x-spaces.conf" "$quad/$name.container.d/"; done
fi
records before-source
bash "$old_tool/source/scripts/deploy/sync-opt-current.sh" --protected-source "$writer" "$source" "$source" "$old_api/source"
mkdir -p /run/systemd/generator
QUADLET_UNIT_DIRS="$quad" /usr/libexec/podman/quadlet /run/systemd/generator
records after-source || { [[ ! -f /tmp/fixture-errors ]] || cat /tmp/fixture-errors >&2; fail 'original activation'; }
failed=$(</tmp/failed-release)
release=$legacy/artifacts/worker-profile/releases/$failed
records after-attempt
: >/tmp/journal-ticks
(while sleep 1; do printf '%s\n' "${EPOCHREALTIME/./}" >>/tmp/journal-ticks; done) &
ticker=$!

stamp() {
  find "$@" -printf '%p %D:%i %m %U:%G %T@ %C@\n' -type f -exec sha256sum {} + | LC_ALL=C sort
}
stable() {
  stamp "$legacy" "$quad" /usr/local/libexec/hololive-bot /etc/systemd/system /etc/nftables.d \
    /run/lock /run/systemd/generator /run/hololive-bot /etc/pgbouncer /docker-entrypoint-initdb.d \
    /etc/stack-secrets/hololive-bot/compose.env /etc/stack-secrets/hololive-bot/worker-profiles/{hololive-api,alarm-worker}.json \
    "$store/$source" "$store/$writer" "$store/transition-$writer-$source"
  for dir in "$old_api" "$old_tool" "$sources/dv-holo-worker-$source"; do
    find "$dir" -maxdepth 1 -type f -printf '%p %D:%i %m %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort
  done
}
authority() {
  stable
  find "$compose_root" "$store" -mindepth 1 -maxdepth 1 -printf '%p %D:%i %m\n' | LC_ALL=C sort
  stamp "$store/active"
}
args=(--supersede-unstarted "$source" "$candidate" "$candidate" "$next" "$writer" "$failed")
publish() { bash "$tool/scripts/deploy/sync-opt-current.sh" "$@"; }
records stage-successor
proof_args=("$source" "$candidate" "$writer" "$failed" "$(<"$stage/stage.accepted")" "$(<"$worker_stage_new/stage.accepted")")
window() {
  local phase=$1 input=/dev/null
  case "$phase" in
    api) input=/tmp/source/transport ;;
    worker) input=/tmp/api/transport ;;
    receipt) input=/tmp/worker/transport ;;
  esac
  python3 -ISB "$tool_stage/source/scripts/deploy/supersession-window.py" "$phase" "${proof_args[@]}" <"${2:-$input}"
}
accept_window() {
  local phase=$1 input=/dev/null central=fixture evidence=/tmp
  local OLD=$source NEW=$candidate RUNNING=$writer FAILED_API_RELEASE=$failed API_MANIFEST=${proof_args[4]} WORKER_MANIFEST=${proof_args[5]}
  local SUPERVISOR_SHA256
  SUPERVISOR_SHA256=$(</case/approved-supervisor)
  export central evidence OLD NEW RUNNING FAILED_API_RELEASE API_MANIFEST WORKER_MANIFEST SUPERVISOR_SHA256
  case "$phase" in api) input=/tmp/source/transport ;; worker) input=/tmp/api/transport ;; receipt) input=/tmp/worker/transport ;; esac
  ssh() { [[ $# == 4 && $1 == -T && $2 == -- && $3 == fixture ]] && /usr/bin/bash -c "$4"; }
  uv() { UV_CACHE_DIR=/tmp/uv UV_PYTHON=/usr/bin/python3 /case/uv "$@"; }
  . /case/bootstrap-command
  if ! . /case/transport-command; then
    for path in "$tool_stage/source.coordinator.exit" "$stage"/coordinator.{exit,diagnostics} \
      "$worker_stage_new"/coordinator.{exit,diagnostics} /tmp/fixture-errors /tmp/unexpected-commands; do
      [[ ! -f "$path" ]] || { printf '%s: ' "$path" >&2; cat "$path" >&2; }
    done
    tail -8 /events >&2
    return 1
  fi
  [[ $(<"/tmp/$phase/process_exit") == 0 ]]
  if [[ $phase == api || $phase == worker ]]; then
    local target=$api
    [[ $phase == api ]] || target=$worker
    local accepted_release
    accepted_release=$(<"$receipt/attempt-$target/release")
    awk -v target="$target" -v previous="$accepted_release/previous" '
      $2 == target && $3 == previous { seen=1 }
      END { exit !seen }
    ' /tmp/transient-profiles || fail "$phase rollback profile job missing"
  fi
}
fresh_attempt() (
  local phase=$1 directory fresh key reservation=$receipt/$1.requested
  case "$phase" in source) directory=$tool_stage ;; api) directory=$stage ;; worker|receipt) directory=$worker_stage_new ;; esac
  fresh=$(mktemp -d /tmp/isolated-attempt.XXXXXX)
  mkdir "$fresh/source" "$fresh-clone"
  mount --bind "$directory/source" "$fresh-clone"
  for key in source.bundle stage.accepted manifest.json candidate-manifests.json pair-binding.requested "$api.candidate-image" "$worker.candidate-image"; do
    [[ ! -f "$directory/$key" ]] || cp --preserve=all "$directory/$key" "$fresh/"
  done
  if [[ "$phase" == receipt ]]; then
    cp -p "$directory"/activation.* "$directory"/coordinator.* "$fresh/"
  fi
  printf '%s\n' "$fresh" >/tmp/isolated-stage
  mount --bind "$fresh" "$directory"
  mount --bind "$fresh-clone" "$directory/source"
  [[ $phase != source ]] || reservation=$sources/source-$source-$candidate.requested
  trap 'if [[ -f $reservation ]]; then mv "$reservation" "$fresh/central-reservation"; fi; umount "$directory/source"; umount "$directory"; umount "$fresh-clone"' EXIT
  if [[ "${2:-}" == detached-publisher ]]; then
    python3 -ISB /case/unstarted-source-case-evidence.py detached-publisher "$tool_stage/source/scripts/deploy/supersession-window.py" source "${proof_args[@]}" </dev/null
  elif [[ "${2:-}" == broken-bundle ]]; then
    : >"$fresh/source.bundle"
    rc=0
    window "$phase" || rc=$?
    [[ $rc == 75 && -f "$fresh/activation.window" && -f "$fresh/activation.not-accepted" ]] || exit 1
    cp "$tool_stage/source.bundle" "$fresh/source.bundle"
    window "$phase"
  else
    window "$@"
  fi
)
expect_status() {
  local expected=$1 status=0
  shift
  "$@" >/tmp/expected-out 2>&1 || status=$?
  [[ "$status" == "$expected" ]] || { tail -5 /tmp/expected-out >&2; fail "expected $expected, got $status from $1"; }
}
expect_reason() {
  local reason=$1
  shift
  expect_status 1 "$@"
  grep -Fq "$reason" /tmp/expected-out || { cat /tmp/expected-out >&2; fail "$reason"; }
}
if [[ -f /tmp/full-history ]]; then
  [[ $source == 6d7e8749983723867fa52d5a0e9cd648198d2e61 && $writer == efafcf4005878f53ff239f8e8a1b6255e3b59b75 ]]
  for helper in sealed-app.sh protected-binds.sh; do
    [[ ! -e "$store/$writer/scripts/deploy/lib/$helper" && -f "$store/$source/scripts/deploy/lib/$helper" ]]
  done
  accept_window source
  : >/tmp/slot-jobs-enabled
  accept_window api
  accept_window worker
fi
if [[ -f /tmp/scenario ]]; then
  scenario=$(</tmp/scenario)
  if [[ "$scenario" == late-* ]]; then
    cp /tmp/scenario /tmp/fault
    expect_status 75 window source
    expected="incomplete $source -> $candidate"
    [[ $scenario != late-restore ]] || expected=$source
    [[ -f /tmp/writer-restarted-at-sync && $(<"$store/active") == "$expected" ]]
    python3 -ISB -c 'import json,sys; assert json.load(open(sys.argv[1]))["returncode"] == 75' "$tool_stage/source.coordinator.exit"
  else
    accept_window source
    : >/tmp/slot-jobs-enabled
    pointer="$legacy/artifacts/worker-profile/active/$api.pair"
    sha256sum "$pointer" "$quad/$api.container.d/50-worker-profile.conf" >/tmp/pair.before
    sha256sum "$overlay" "/run/systemd/generator/$migrate.service" >/tmp/migrator.before
    reloads_before=$(grep -c '^systemctl daemon-reload$' /events)
    cp /tmp/scenario /tmp/fault
    expect_status 75 window api
    if [[ $scenario == unaccounted-install || $scenario == pending-nop ]]; then
      expected='unaccounted manager job installation'
      [[ $scenario == unaccounted-install ]] || expected='pending manager allocation witness'
      grep -Fq "$expected" /tmp/expected-out || { cat /tmp/expected-out >&2; exit 1; }
      [[ ! -e "$stage/activation.accepted" ]]
      printf '[PASS] %s refused\n' "$scenario"
      exit
    fi
    if [[ $scenario == writer-native-dropin ]]; then
      grep -Fq "unexpected native unit override: $worker" /tmp/expected-out || { cat /tmp/expected-out >&2; exit 1; }
      sha256sum -c --quiet /tmp/migrator.before
      [[ $(grep -c '^systemctl daemon-reload$' /events) == "$reloads_before" ]] && ! grep -q 'EnqueueUnitJob sss hololive-db-migrate' /events &&
        [[ ! -e "$stage/activation.accepted" ]] || exit 1
      printf '[PASS] writer definition refused before overlay exchange\n'
      exit
    fi
    if [[ $scenario == dependency-job ]]; then
      grep -rFq 'has a pending job: holo-pgbouncer.service' /tmp/expected-out "$stage" || { cat /tmp/expected-out >&2; exit 1; }
      ! grep -q 'EnqueueUnitJob sss hololive-db-migrate' /events && [[ ! -e "$stage/activation.accepted" ]] || exit 1
      printf '[PASS] pending dependency job refused at the request boundary\n'
      exit
    fi
    if [[ $scenario == missing-optional ]]; then
      [[ -f /tmp/removed-optional && ! -e "$quad/$worker.container.d/40-x-spaces.conf" ]] && ! grep -q 'EnqueueUnitJob sss hololive-api' /events || exit 1
      printf '[PASS] missing optional drop-in refused before writer restart\n'
      exit
    fi
    if [[ $scenario == health-at-sync || $scenario == external-reload ]]; then
      [[ -f /tmp/dispatch-race ]] && ! grep -q 'EnqueueUnitJob sss hololive-db-migrate' /events || exit 1
      if [[ $scenario == health-at-sync ]]; then [[ $(grep -c '^systemctl daemon-reload$' /events) == "$reloads_before" ]]; fi
      printf '[PASS] %s rejected before schema dispatch\n' "$scenario"
      exit
    fi
    if [[ $scenario == lost-response || $scenario == pending-during-peer ||
          $scenario == run-before-config || $scenario == merge-extra-run ]]; then
      records_path=$(<"$store/transition-$source-$candidate/attempt-$api/release")
      [[ ! -e "$records_path/writer-job/anchor" && ! -e "$stage/activation.accepted" ]]
      if [[ $scenario == pending-during-peer ]]; then
        [[ -f /tmp/racing-pending ]] && ! grep -q 'EnqueueUnitJob sss hololive-db-migrate' /events || exit 1
      elif [[ $scenario == lost-response ]]; then
        [[ $(grep -c 'EnqueueUnitJob sss hololive-db-migrate' /events) == 1 && ! -f "$records_path/schema-job/anchor" ]]
        python3 -ISB -c 'import json; assert json.load(open("/tmp/observations"))["pending_job"]["unit"]=="hololive-db-migrate.service"'
      else
        [[ ! -f "$records_path/activation.intent" ]]
        if [[ $scenario == merge-extra-run ]]; then
          [[ -s /tmp/merged-running ]]
        else
          [[ -f /tmp/direct-container ]]
        fi
      fi
      printf '[PASS] %s preserved the single request boundary\n' "$scenario"
      exit
    fi
    if [[ $scenario == merged-* ]]; then
      [[ -f /tmp/merged-job && ! -e "$stage/activation.accepted" ]]
      python3 -ISB -c 'import json; r=json.load(open("/tmp/merged-job")); assert r["requested"] > r["anchor"]'
      attempted=$(<"$store/transition-$source-$candidate/attempt-$api/release")
      if [[ $scenario == merged-schema ]]; then
        [[ -f "$attempted/schema-job/allocation" && ! -e "$attempted/schema-job/anchor" && ! -e "$attempted/writer-job" ]]
      else
        [[ -f "$attempted/writer-job/allocation" && ! -e "$attempted/writer-job/anchor" && ! -e "$attempted/accepted-writer" ]]
      fi
      printf '[PASS] %s rejected without accepting merged work\n' "$scenario"
      exit
    fi
    [[ -f /tmp/observed-pending-job && ! -f /tmp/reloaded-with-pending-job ]]
    attempted=$(<"$store/transition-$source-$candidate/attempt-$api/release")
    [[ ! -d "$attempted/writer-job" ]]
    if [[ "$scenario" == pending-worker ]]; then
      [[ -f "$attempted/schema-job/request" && ! -f "$attempted/schema-job/anchor" ]]
    elif [[ $scenario == pre-overlay-job ]]; then
      sha256sum -c /tmp/migrator.before
    elif [[ $scenario == pre-activate-job ]]; then
      sha256sum -c /tmp/pair.before
    else
      [[ -f "$attempted/schema-check" && -f "$attempted/activation.intent" ]]
    fi
  fi
  printf '[PASS] %s refused at the publication or dispatch boundary\n' "$scenario"
  exit
fi
reject() {
  local name=$1 status=0 expected queried
  shift
  authority >/tmp/state.before
  : >/events
  case "$name" in
    direct-publisher) fresh_attempt source detached-publisher >/tmp/refusal 2>&1 || status=$? ;;
    strict-protected-source) publish "$@" >/tmp/refusal 2>&1 || status=$? ;;
    stale-manager|extra-condition|native-overlay) fresh_attempt source >/tmp/refusal 2>&1 || status=$? ;;
    *) python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed" >/tmp/refusal 2>&1 || status=$? ;;
  esac
  if [[ $status == 0 ]]; then cat /tmp/refusal >&2; fail "$name: expected refusal"; fi
  case "$name" in
    stale-manager|extra-condition|native-overlay)
      [[ ! -e "$(< /tmp/isolated-stage)/central-reservation" && ! -e "$(< /tmp/isolated-stage)/source.requested" ]] \
        || fail "$name: proof refusal consumed the source window" ;;
  esac
  case "$name" in
    strict-protected-source) expected='schema-check' ;;
    direct-publisher) [[ $status == 75 ]]; expected='^\[sync-opt-current\] ERROR: no live supervisor for this source publication$' ;;
    missing\ *) expected='unstarted source evidence unavailable' ;;
    unknown-journal) expected='journal scan incomplete' ;;
    unknown-outcome) expected='coordinator did not refuse before activation' ;;
    unexpected-job) expected='pre-job release evidence' ;;
    restored-release) expected='selected release outside coordinator window' ;;
    worker-attempt) expected='unexpected activation evidence' ;;
    missing-coverage) expected='missing manager journal coverage' ;;
    missing-journal-segment) expected='missing original journal segment' ;;
    "$api"|"$worker"|"$migrate"|unknown-job-unit) expected='possible schema or writer job' ;;
    writer-changed) expected="protected generation changed: $worker" ;;
    extra-condition|native-overlay) expected='미수락' ;;
    manager-debug) expected='possible schema or writer job' ;;
    journald-restart) expected='journal daemon changed across evidence interval' ;;
    no-manager-pid|no-manager-uid) expected='unidentified protected manager record' ;;
    no-podman-uid) expected='unidentified protected container sender' ;;
    entry-printing-failed|kmsg-read-failed) expected='possible manager journal loss' ;;
    kernel-audit-start) expected='unexpected protected service audit record' ;;
    migrator-completion-row) expected='unexpected rootful container execution' ;;
    protected-audit) expected='unexpected protected service audit record' ;;
    credential-loss|ring-loss|suppressed-manager|journal-store-failure|unexpected-write-error) expected='possible manager journal loss' ;;
    anonymous-manager-start|kmsg-manager-start|unattributed-kmsg|unattributed-reload|unknown-manager-observation|skipped-via-kmsg) expected='possible schema or writer job' ;;
    audit-only-close|unresolved-manager-pool|notice-only) expected='missing manager journal coverage' ;;
    copied-original) expected='original receipt identity changed' ;;
    copied-worker-release) expected='another writer release changed' ;;
    copied-worker-revision) expected='another writer attempt' ;;
    original-source-refused) expected='unexpected activation evidence: source.not-accepted' ;;
    switched-sequence) expected='journal sequence changed' ;;
    late-source-exit) expected='original source publication did not succeed' ;;
    rewritten-overlay) expected='migrator was not replaced during original attempt' ;;
    stale-manager) expected='미수락' ;;
    substituted-protected-bind) expected='(protected bind|protected mount|unrecognized protected mount)' ;;
    *) fail "unknown test case: $name" ;;
  esac
  grep -Eq "$expected" /tmp/refusal || { cat /tmp/refusal >&2; fail "$name: unrelated rejection"; }
  case "$name" in
    stale-manager) queried='Service ExecStart$' ;;
    native-overlay) queried='Unit DropInPaths$' ;;
    extra-condition) queried='Service ExecCondition$' ;;
    *) queried='' ;;
  esac
  [[ -z "$queried" ]] || grep -Eq "$queried" /events || { tail -15 /events >&2; fail "$name: loaded unit was never inspected"; }
  authority >/tmp/state.after
  cmp /tmp/state.before /tmp/state.after || fail "$name: refusal changed host authority"
  [[ $(<"$store/active") == "$source" && ! -e $store/$candidate ]] || fail "$name: refusal published a successor"
  [[ ! -e /tmp/unexpected-commands ]] || { cat /tmp/unexpected-commands >&2; fail "$name: unexpected host operation"; }
  printf '[PASS] refusal: %s\n' "$name"
}
if [[ ! -f /tmp/full-history ]]; then
python3 -ISB -c 'import runpy,sys; print(*runpy.run_path(sys.argv[1])["prove"](*sys.argv[2:]), sep="\n")' \
  "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed" >/tmp/original-proof || {
    [[ ! -f /tmp/fixture-errors ]] || cat /tmp/fixture-errors >&2
    fail 'original proof'
  }
cmp /tmp/original-proof <(cat "/tmp/$api.generation" "/tmp/$worker.generation")
reject strict-protected-source --protected-source "$source" "$candidate" "$candidate" "$next"
reject direct-publisher "${args[@]}"
saved_api_seal=${proof_args[4]}
saved_worker_seal=${proof_args[5]}
proof_args[4]=$saved_worker_seal proof_args[5]=$saved_api_seal
expect_status 75 window precheck
proof_args[4]=$saved_api_seal proof_args[5]=$saved_worker_seal
printf 'notice-only\n' >/tmp/fault
expect_status 75 window precheck
printf 'restarting-migrator\n' >/tmp/fault
expect_status 75 window precheck
: >/tmp/fault
cp -p "$old_api/coordinator.exit" /tmp/precheck-exit
mount --bind /tmp/precheck-exit "$old_api/coordinator.exit"
expect_status 75 window precheck
umount "$old_api/coordinator.exit"
prior_version="$store/transition-$writer-$source/previous.actual/VERSION"
cp -p "$prior_version" /tmp/prior-version
printf 'X' | dd of=/tmp/prior-version bs=1 seek=0 conv=notrunc status=none
mount --bind /tmp/prior-version "$prior_version"
expect_status 75 window precheck
umount "$prior_version"
[[ ! -e "$tool_stage/source.requested" && ! -e "$tool_stage/source.window" ]]
window precheck
[[ ! -e "$tool_stage/source.requested" && ! -e "$tool_stage/source.window" && $(<"$store/active") == "$source" ]]
chmod 0755 "$worker_stage_new"
expect_status 75 fresh_attempt source
[[ ! -e "$(< /tmp/isolated-stage)/source.requested" && ! -e "$(< /tmp/isolated-stage)/source.not-accepted" && ! -e "$(< /tmp/isolated-stage)/central-reservation" ]]
chmod 0700 "$worker_stage_new"
printf 'publisher-sync-failure\n' >/tmp/fault
expect_status 75 fresh_attempt source
: >/tmp/fault
[[ -f "$(< /tmp/isolated-stage)/source.requested" && -f "$(< /tmp/isolated-stage)/source.not-accepted" && -f "$(< /tmp/isolated-stage)/central-reservation" ]]
cp "$(< /tmp/isolated-stage)/central-reservation" "$sources/source-$source-$candidate.requested"
expect_status 75 fresh_attempt source
[[ ! -e "$(< /tmp/isolated-stage)/source.requested" && -f "$(< /tmp/isolated-stage)/central-reservation" ]]
unshare --mount --propagation private bash -eu -s -- "$old_api" "$tool" "$source" "$writer" "$failed" <<'MASK'
  old_api=$1 tool=$2 source=$3 writer=$4 failed=$5
  mkdir /tmp/original-stage /tmp/masked-original
  mount --rbind "$old_api" /tmp/original-stage
  mount --bind /tmp/masked-original "$old_api"
  for entry in /tmp/original-stage/*; do
    dest="$old_api/${entry##*/}"
    if [[ -d $entry ]]; then mkdir "$dest"; else touch "$dest"; fi
    mount --bind "$entry" "$dest"
  done
  python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed" >/tmp/masked-control
  cmp /tmp/original-proof /tmp/masked-control
  for name in coordinator.exit activation.not-accepted coordinator.diagnostics; do
    umount "$old_api/$name"
    rm "$old_api/$name"
    rc=0
    python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed" >/tmp/missing-receipt 2>&1 || rc=$?
    [[ $rc == 1 ]] && grep -Fxq "unstarted source evidence rejected: missing original receipt: $old_api/$name" /tmp/missing-receipt || exit 1
    touch "$old_api/$name"
    mount --bind "/tmp/original-stage/$name" "$old_api/$name"
  done
MASK
exit_record=$old_api/coordinator.exit
printf '{"rc":1,"markers":["outcome_unknown"]}\n' >/tmp/unknown-exit
touch -r "$exit_record" /tmp/unknown-exit
mount --bind /tmp/unknown-exit "$exit_record"
reject unknown-outcome "${args[@]}"
umount "$exit_record"
source_exit=$old_tool/source.coordinator.exit
python3 -ISB -c 'import json,os,sys; d=json.load(open(sys.argv[1])); d["ended_utc_ns"]=os.stat(sys.argv[2]).st_mtime_ns+1; print(json.dumps(d))' \
  "$source_exit" "$old_tool/source.accepted" >/tmp/late-source-exit
touch -r "$source_exit" /tmp/late-source-exit
mount --bind /tmp/late-source-exit "$source_exit"
reject late-source-exit "${args[@]}"
umount "$source_exit"
cp -p "$overlay" /tmp/rewritten-overlay
mount --bind /tmp/rewritten-overlay "$overlay"
reject rewritten-overlay "${args[@]}"
umount "$overlay"
for job in schema-job writer-job; do
  mount --bind "/tmp/$job-case" "$release"
  reject unexpected-job
  printf 'unknown\n' >"$release/$job/request"
  reject unexpected-job
  umount "$release"
done
cp -a "$release" /tmp/restored-release
mount --bind /tmp/restored-release "$release"
reject restored-release
umount "$release"
worker_stage=$sources/dv-holo-worker-$source
touch "$worker_stage/activation.requested"
reject worker-attempt "${args[@]}"
rm "$worker_stage/activation.requested"
touch "$old_tool/source.not-accepted"
reject original-source-refused
rm "$old_tool/source.not-accepted"
cp -p "$old_api/coordinator.diagnostics" /tmp/restored-diagnostic
mount --bind /tmp/restored-diagnostic "$old_api/coordinator.diagnostics"
reject copied-original
umount "$old_api/coordinator.diagnostics"
prior_worker=$(<"$legacy/artifacts/worker-profile/active/$worker.pair")
cp -a "${prior_worker%/candidate}" /tmp/prior-worker-release
mount --bind /tmp/prior-worker-release "${prior_worker%/candidate}"
reject copied-worker-release
umount "${prior_worker%/candidate}"
worker_revision="${prior_worker%/candidate}/candidate.revision"
cp -p "$worker_revision" /tmp/copied-worker-revision
mount --bind /tmp/copied-worker-revision "$worker_revision"
reject copied-worker-revision
umount "$worker_revision"
printf '%s\n' "${EPOCHREALTIME/./}" >/tmp/direct-container
expect_reason 'unexpected rootful container execution' python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed"
rm /tmp/direct-container
mkdir -p /run/systemd/journald.conf.d
printf '[Journal]\nRateLimitIntervalSec=0\n' >/run/systemd/journald.conf.d/50-unapplied.conf
expect_reason 'original journal policy unavailable' python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed"
rm -r /run/systemd/journald.conf.d
python3 -ISB - "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed" <<'PY'
import os
import runpy
import sys
import time
from pathlib import Path
module = runpy.run_path(sys.argv[1])
scope = module['prove'].__globals__
refusal = scope['Refusal']
original_limit = scope['manager_rate_limit']
def proven(burst):
    scope['manager_rate_limit'] = lambda start_ns, daemon: (30_000_000, burst)
    try:
        scope['prove'](*sys.argv[2:])
        return True
    except refusal as error:
        assert str(error) == 'manager rate-limit pool may have saturated', error
        return False
low, high = 1, 10_000
assert proven(high) and not proven(low)
while high - low > 1:
    middle = (low + high) // 2
    low, high = (low, middle) if proven(middle) else (middle, high)
assert proven(high) and not proven(high - 1)
scope['manager_rate_limit'] = original_limit
scope['loaded_before'] = lambda path, loaded_ns: True
def limits(text, start_ns=None):
    scope['run'] = lambda *args: text
    return original_limit(time.time_ns() if start_ns is None else start_ns, ('a' * 32, 0))
def refused(reason, *args):
    try:
        limits(*args)
    except refusal as error:
        assert str(error) == reason, error
    else:
        raise AssertionError(reason)
assert limits('[Journal]\nRateLimitIntervalSec=250ms\nRateLimitBurst=40\n') == (250_000, 40)
assert limits('[Journal]\nRateLimitInterval=2min\nRateLimitIntervalSec=1.5s\n') == (1_500_000, 10_000)
assert limits('[Journal]\nRateLimitBurst=40\n[Other]\nRateLimitBurst=1\n') == (30_000_000, 40)
refused('unknown manager journal rate interval', '[Journal]\nRateLimitIntervalSec=3 weeks\n')
units = Path('/run/systemd/units')
units.mkdir(parents=True, exist_ok=True)
link = units / 'log-rate-limit-burst:init.scope'
os.symlink('7', link)
assert limits('[Journal]\nRateLimitBurst=40\n') == (30_000_000, 7)
refused('original init.scope rate limit unavailable', '[Journal]\n', 0)
link.unlink()
PY
for fault in application-queue daemon-reload original-cancel unrelated-audit pid1-avc firewall-drop permission-warning corrupt-journal-file; do
  printf '%s\n' "$fault" >/tmp/fault
  python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed" >/dev/null
done
for fault in credential-loss ring-loss journald-restart protected-audit no-manager-pid no-manager-uid no-podman-uid skipped-via-kmsg notice-only unexpected-write-error manager-debug suppressed-manager anonymous-manager-start kmsg-manager-start unattributed-kmsg unattributed-reload unknown-manager-observation journal-store-failure entry-printing-failed kmsg-read-failed kernel-audit-start migrator-completion-row audit-only-close unresolved-manager-pool unknown-journal missing-coverage missing-journal-segment unknown-job-unit switched-sequence "$migrate" "$api" "$worker" writer-changed stale-manager native-overlay extra-condition; do
  printf '%s\n' "$fault" >/tmp/fault
  reject "$fault" "${args[@]}"
done
: >/tmp/fault
hba=/run/hololive-bot/postgres/pg_hba.conf
cp --preserve=all "$hba" /tmp/substituted-hba
mount --bind /tmp/substituted-hba "$hba"
mount -o remount,bind,ro "$hba"
reject substituted-protected-bind "${args[@]}"
umount "$hba"
cp /tmp/ledger.db /tmp/rewritten-ledger.db
python3 -ISB -c 'import sqlite3; db=sqlite3.connect("/tmp/rewritten-ledger.db"); db.execute("UPDATE schema_migrations SET xmin=xmin+1"); db.commit()'
mount --bind /tmp/rewritten-ledger.db /tmp/ledger.db
expect_reason 'original migration ledger or catalog changed' python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed"
umount /tmp/ledger.db
cp /tmp/ledger.db /tmp/invalid-index.db
python3 -ISB -c 'import sqlite3; db=sqlite3.connect("/tmp/invalid-index.db"); db.execute("INSERT INTO pg_index VALUES(0,1,1)"); db.commit()'
mount --bind /tmp/invalid-index.db /tmp/ledger.db
expect_reason 'original migration ledger or catalog changed' python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed"
umount /tmp/ledger.db
cp /tmp/ledger.db /tmp/catalog.db
python3 -ISB -c 'import sqlite3; db=sqlite3.connect("/tmp/catalog.db"); assert db.execute("DELETE FROM privilege WHERE role=? AND object=? AND action=?", ("hololive_alarm_worker","public.alarms","SELECT")).rowcount == 1; db.commit()'
mount --bind /tmp/catalog.db /tmp/ledger.db
expect_reason 'original migration ledger or catalog changed' python3 -ISB "$tool/scripts/deploy/lib/unstarted-source.py" "$source" "$writer" "$failed"
umount /tmp/ledger.db
active_api=$(<"$legacy/artifacts/worker-profile/active/$api.pair")
ln -s /tmp "${active_api%/candidate}/previous/untrusted"
expect_status 75 window precheck
[[ ! -e "$tool_stage/source.requested" && ! -e "$tool_stage/source.window" ]]
rm "${active_api%/candidate}/previous/untrusted"
for protected_path in "$active_api" "$active_api/profile.json"; do
  mode=$(stat -c %a "$protected_path")
  if [[ -d $protected_path ]]; then chmod 0700 "$protected_path"; else chmod 0600 "$protected_path"; fi
  expect_status 75 window precheck
  chmod "$mode" "$protected_path"
done
mkdir "$store/$candidate"
expect_status 75 window precheck
rmdir "$store/$candidate"
[[ ! -e "$tool_stage/source.requested" && ! -e "$tool_stage/source.window" ]]
stable >/tmp/runtime.before
accept_window source
stable >/tmp/runtime.after
cmp /tmp/runtime.before /tmp/runtime.after || fail 'forward publication changed a predecessor or runtime authority'
cmp "$receipt/revisions" <(printf 'installed=%s\ncandidate=%s\ntool=%s\nrunning=%s\n' "$source" "$candidate" "$candidate" "$writer")
cmp "$receipt/predecessor.writers" <(cat "/tmp/$api.generation" "/tmp/$worker.generation")
cmp "$receipt/pairs.before" "$receipt/pairs.after"
cmp "$receipt/protected.before" "$receipt/protected.after"
cmp "$receipt/images" "$receipt/images.after"
cmp "$receipt/previous.metadata" "$receipt/previous.after"
cmp "$receipt/previous.actual/.source-revision" "$store/$source/.source-revision"
cmp "$store/$candidate/hololive/hololive-api/VERSION" "$next/hololive/hololive-api/VERSION"
[[ $(<"$legacy/.source-revision") == "$writer" && $(<"$store/active") == "$candidate" && $(<"$store/$candidate/.source-revision") == "$candidate" ]] || fail 'incorrect forward publication stamps'
[[ ! -e /tmp/unexpected-commands ]] || { cat /tmp/unexpected-commands >&2; fail 'unexpected host operation'; }
printf '[PASS] old writers, original refusal, and independently loaded check survived forward-only source publication\n'

coordinator="$store/$candidate/scripts/deploy/quadlet-redeploy-service.sh"
api_image=$(<"$stage/$api.candidate-image")
worker_image=$(<"$stage/$worker.candidate-image")
reject_slot() {
  local label=$1
  shift
  stamp "$receipt" "$legacy/artifacts/worker-profile" "$quad" | sed "\|^$receipt |d" > /tmp/slot.before
  if "$@" >/tmp/slot-refusal 2>&1; then fail "$label: slot admitted"; fi
  stamp "$receipt" "$legacy/artifacts/worker-profile" "$quad" | sed "\|^$receipt |d" > /tmp/slot.after
  cmp /tmp/slot.before /tmp/slot.after || fail "$label: slot refusal mutated state"
  printf '[PASS] slot refusal: %s\n' "$label"
}
expect_status 75 records unreadable-refusal
expect_status 75 fresh_attempt api broken-bundle
python3 -ISB -c 'import json,sys; d=json.load(sys.stdin); d["output_nonce"]="a"*32; print(json.dumps(d,sort_keys=True,separators=(",",":")))' </tmp/source/transport >/tmp/reconstructed
expect_status 75 fresh_attempt api /tmp/reconstructed
[[ ! -e "$(< /tmp/isolated-stage)/activation.window" ]]
unexpected_schema() {
  local name=$1
  printf '%s\n' "${EPOCHREALTIME/./}" >/tmp/extra-schema-event
  expect_reason 'possible schema or writer job' records migrator-entry "$name"
  rm /tmp/extra-schema-event
}
unexpected_schema "$api"
printf '%s\n' "${EPOCHREALTIME/./}" >/tmp/direct-container
expect_reason 'unexpected rootful container execution' records migrator-entry "$api"
rm /tmp/direct-container
printf 'unknown-allocator\n' >/tmp/fault
expect_reason 'unreviewed manager job allocator' records migrator-entry "$api"
: >/tmp/fault
printf '%s\n' "${EPOCHREALTIME/./}" >/tmp/unrelated-install
records migrator-entry "$api"
printf '%s 8900 starting\n' "${EPOCHREALTIME/./}" >>/tmp/transient-jobs
printf '%s 8900 done\n' "${EPOCHREALTIME/./}" >>/tmp/transient-jobs
records migrator-entry "$api"
touch /tmp/silent-cancel
records migrator-entry "$api"
rm /tmp/silent-cancel /tmp/unrelated-install
for fault in quiet-manager quiet-unit; do
  printf '%s\n' "$fault" >/tmp/fault
  expect_reason 'protected manager records may be suppressed' records migrator-entry "$api"
done
: >/tmp/fault
mv "$tool_stage/source.accepted" /tmp/confirmed-source
reject_slot missing-source-confirmation fresh_attempt api
[[ -f "$(< /tmp/isolated-stage)/activation.window" && -f "$(< /tmp/isolated-stage)/activation.not-accepted" ]]
mv /tmp/confirmed-source "$tool_stage/source.accepted"
saved=$(</tmp/isolated-stage)
cp "$saved/central-reservation" "$receipt/api.requested"
expect_status 75 fresh_attempt api
[[ ! -e "$(</tmp/isolated-stage)/activation.window" ]]
original_seal=${proof_args[4]}
proof_args[4]=$(printf '%064d' 23)
reject_slot wrong-image-manifest fresh_attempt api
proof_args[4]=$original_seal
reject_slot premature-worker "$coordinator" "$worker" "$worker_image" "$api_image"
reject_slot premature-worker-transport fresh_attempt worker /tmp/source/transport
mount --bind /tmp/substituted-hba "$hba"
mount -o remount,bind,ro "$hba"
reject_slot protected-bind-drift fresh_attempt api
umount "$hba"
mkdir -m0700 "$receipt/attempt-$api"
ambiguous="${release%/*}/$api-v2-$(date -u +%Y%m%dT%H%M%SZ)-partial"
mkdir -m0700 "$ambiguous" "$ambiguous/schema-job"
printf '%s\n' "$ambiguous" >"$receipt/attempt-$api/release"
printf '%s\n' "$api" >"$ambiguous/service"
printf '%s\n' "$candidate" >"$ambiguous/candidate.revision"
printf 'start %s.service %s %s\n' "$migrate" "$api_image" "$EPOCHREALTIME" >"$ambiguous/schema-job/request"
reject_slot schema-reissue fresh_attempt api
mv "$ambiguous" "$receipt/attempt-$api" /tmp
: >/tmp/slot-jobs-enabled
reject_slot unacknowledged-source-output fresh_attempt api /tmp/source/stdout
reject_slot empty-source-acknowledgment fresh_attempt api /dev/null
printf 'merge-during-run\n' >/tmp/fault
accept_window api
[[ -s /tmp/merged-running ]]
: >/tmp/fault
[[ -f "$stage/activation.accepted" ]]
mv "$stage/activation.accepted" /tmp/confirmed-api
reject_slot incomplete-api-acknowledgment fresh_attempt worker
mv /tmp/confirmed-api "$stage/activation.accepted"
ordinary_admission() {
  bash -eu -o pipefail -s -- "$store/$candidate" "$legacy" "$quad" "$worker" "$api" "$candidate" "$api_image" "$worker_image" <<'BASH'
cd "$1"
. scripts/deploy/lib/sealed-app.sh
. scripts/deploy/lib/postgres-endpoint.sh
validator="$1/scripts/deploy/lib/app-source-authority.py"
pair_tool="$1/scripts/deploy/lib/worker-profile-pair.py"
deploy_root=$2
releases=$2/artifacts/worker-profile/releases
quadlet_dir=$3
service=$4
unchanged_peer=$5
revision=$6
migrator_id=$7
prefix=
peer_generation=$(sealed_generation "$unchanged_peer")
store=/case/ordinary
sealed_peer_admission "$8"
BASH
}
(
  dir="/case/ordinary/transition-$source-$candidate"
  unused=/case/unused-sources
  mkdir -p "$dir" "$unused"
  mount --bind "$unused" "$sources"
  trap 'umount "$sources"' EXIT
  printf 'installed=%s\ncandidate=%s\ntool=%s\n' "$source" "$candidate" "$candidate" >"$dir/revisions"
  ordinary_admission || fail 'ordinary peer control was refused'
  mkdir "$dir/attempt-$worker"
  expect_reason 'ordinary predecessor has recovery evidence' ordinary_admission
  rmdir "$dir/attempt-$worker"
  for evidence in "dv-tool-$candidate/source.window" "dv-holo-api-$candidate/activation.window" \
      "dv-holo-worker-$candidate/activation.window" "source-$source-$candidate.requested"; do
    mkdir -p "$(dirname "$unused/$evidence")"
    touch "$unused/$evidence"
    expect_reason 'ordinary predecessor has recovery evidence' ordinary_admission
    rm "$unused/$evidence"
  done
)
unexpected_schema "$worker"
python3 -ISB - "$stage" /tmp/replaced-migrator "$overlay" <<'PY'
import json
from pathlib import Path
import sys

stage, copy, installed = map(Path, sys.argv[1:])
start = json.loads((stage / 'activation.requested').read_bytes())['utc_ns']
schema = json.loads((stage / 'activation.accepted').read_bytes())['proof']['schema_job']
assert start <= copy.stat().st_ctime_ns <= schema['request_utc_ns']
assert copy.read_bytes() == installed.read_bytes() and not copy.samefile(installed)
PY
mount --bind /tmp/replaced-migrator "$overlay"
expect_reason 'accepted API migrator definition was replaced' records migrator-entry "$worker"
umount "$overlay"
accept_window worker
completed=(python3 -ISB "$store/$candidate/scripts/deploy/lib/unstarted-source.py" --completed "$source" "$candidate")
reject_slot missing-final-transport "${completed[@]}"
fi
follow=$sources/follow/source
follow_revision=$(git -C "$follow" rev-parse HEAD)
follow_tool=$sources/follow-tool/source
follow_tool_revision=$(git -C "$follow_tool" rev-parse HEAD)
follow_receipt=$store/transition-$candidate-$follow_revision
for name in "$api" "$worker"; do
  number=11
  [[ "$name" == "$api" ]] || number=12
  printf 'sha256:%064d\n' "$number" >"${follow%/source}/$name.candidate-image"
done
python3 -ISB -c 'import json,sys; from pathlib import Path; p=Path("/tmp/observations"); s=json.loads(p.read_bytes()); s["images"].update({f"{i:064d}":[n,sys.argv[1]] for i,n in enumerate(sys.argv[2:],11)}); p.write_text(json.dumps(s))' "$follow_revision" "$api" "$worker"
publish_follow() {
  bash "$follow_tool/scripts/deploy/sync-opt-current.sh" --protected-source "$candidate" "$follow_revision" "$follow_tool_revision" "$follow"
}
refuse_follow() {
  local expected=$1
  if publish_follow >/tmp/follow.out 2>&1; then fail "follow-up unexpectedly published"; fi
  grep -Fq "$expected" /tmp/follow.out || { tail -8 /tmp/follow.out >&2; fail "unrelated follow-up refusal"; }
  cmp "$store/active" <(printf '%s\n' "$candidate")
  [[ ! -e "$follow_receipt" && ! -e "$store/$follow_revision" ]]
}
refuse_follow 'original recovery windows have no verified terminal acceptance'
if [[ -f /tmp/full-history ]]; then
  accept_window receipt
else
worker_slot="$receipt/attempt-$worker/release"
(
  exec 7</run/lock/hololive-quadlet-redeploy.lock
  flock -n 7
  expect_status 75 fresh_attempt receipt
  saved=$(</tmp/isolated-stage)
  [[ ! -e "$saved/central-reservation" && ! -e "$saved/activation.confirmation-not-accepted" && ! -e "$saved/activation.confirmation.requested" ]]
)
printf 'stale reservation\n' >"$receipt/receipt.requested"
expect_status 75 fresh_attempt receipt
[[ ! -e "$(< /tmp/isolated-stage)/activation.confirmation.requested" ]]
installed="$quad/$worker.container.d/50-worker-profile.conf"
cp -p "$installed" /tmp/restored-dropin
printf '\n' >>/tmp/restored-dropin
mount --bind /tmp/restored-dropin "$installed"
expect_status 75 fresh_attempt receipt
umount "$installed"
cp -p "$overlay" /tmp/worker-overlay
mount --bind /tmp/worker-overlay "$overlay"
expect_status 75 fresh_attempt receipt
umount "$overlay"
for fault in late-receipt-stage late-receipt-transition late-receipt-run late-receipt-writer late-receipt-manager late-receipt-lock wrong-run-image stale-manager wrong-stop-invocation foreign-container-start writer-native-dropin writer-effective-pre missing-stop reordered-stop; do
  printf '%s\n' "$fault" >/tmp/fault
  expect_status 75 fresh_attempt receipt
  if [[ $fault == late-receipt-* ]]; then
    grep -Fq '"confirmed":"worker"' /tmp/expected-out
    [[ ! -e "$(< /tmp/isolated-stage)/activation.confirmed" ]]
  fi
  rm -f /tmp/receipt-checked /tmp/direct-container
done
: >/tmp/fault
mv "$worker_slot" /tmp/worker-slot-link
expect_status 75 fresh_attempt receipt
[[ -f "$(< /tmp/isolated-stage)/activation.confirmation-not-accepted" ]]
mv /tmp/worker-slot-link "$worker_slot"
printf 'worker-not-ready\n' >/tmp/fault
expect_status 75 fresh_attempt receipt
[[ -f /tmp/readiness-refused ]]
: >/tmp/fault
rc=0
fresh_attempt receipt >/dev/full 2>/tmp/closed-stdout || rc=$?
[[ $rc == 75 && ! -e "$(< /tmp/isolated-stage)/activation.confirmed" && -f "$(< /tmp/isolated-stage)/activation.confirmation-not-accepted" ]] ||
  fail 'confirmation committed before receipt stdout'
accept_window receipt
window receipt-check /tmp/receipt/stdout
python3 -ISB -c 'import json,sys; d=json.load(sys.stdin); d["output_nonce"]="0"*32; print(json.dumps(d,sort_keys=True,separators=(",",":")))' </tmp/receipt/stdout >/tmp/stale-confirmation
expect_status 75 window receipt-check /tmp/stale-confirmation
"${completed[@]}"
for name in pairs.before pairs.after; do
  cp -p "$receipt/$name" "/tmp/$name"
  mount --bind "/tmp/$name" "$receipt/$name"
done
expect_status 1 "${completed[@]}"
for name in pairs.before pairs.after; do umount "$receipt/$name"; done
[[ -f "$worker_stage_new/activation.accepted" && -f "$receipt/attempt-$worker/release" ]]
[[ ! -e /tmp/unexpected-commands ]] || { cat /tmp/unexpected-commands >&2; fail 'unexpected slot host operation'; }
worker_release=$(<"$worker_slot")
intent_copy=$(mktemp -d /tmp/pair-intent.XXXXXX)
old_pointer="$intent_copy/artifacts/worker-profile/active/$worker.pair"
old_dropin="$intent_copy/$worker.container.d/50-worker-profile.conf"
install -d "${old_pointer%/*}" "${old_dropin%/*}" "$intent_copy/release"
cp -a "$worker_release"/{previous,candidate} "$intent_copy/release/"
write_fixture_intent() {
  bash -eu -o pipefail -c '
    ROOT_DIR=$1 deploy_root=$2 quadlet_dir=$2 release=$2/release service=$3 recovery_receipt=$4 original_target_generation=$5
    . "$ROOT_DIR/scripts/deploy/lib/sealed-app.sh"
    sealed_write_intent
  ' _ "$store/$candidate" "$intent_copy" "$worker" "$receipt" "$(<"$worker_release/before.generation")"
}
for part in "$old_pointer" "$old_dropin"; do
  cp "$worker_release/before.pair" "$old_pointer"
  cp "$worker_release/before.conf" "$old_dropin"
  printf 'restored old definition\n' >"$part"
  expect_reason 'original writer pair changed before intent' write_fixture_intent
  [[ ! -e "$intent_copy/release/activation.intent" ]]
done
cp "$worker_release/before.conf" "$old_dropin"
write_fixture_intent
cmp "$intent_copy/release/activation.intent" "$worker_release/activation.intent"

for altered in previous/profile.json candidate/profile.json previous candidate; do
  file="$worker_release/$altered"
  mode=$(stat -c %a "$file")
  if [[ -d "$file" ]]; then chmod 0700 "$file"; else chmod 0600 "$file"; fi
  expect_reason 'unreadable mounted profile pair' "${completed[@]}"
  chmod "$mode" "$file"
done
for dir in "$worker_release" "$(<"$receipt/attempt-$api/release")"; do
  mkdir "$dir/recovery-attempt"
  printf 'apply\n' >"$dir/recovery-attempt/request"
  sync -f "$dir/recovery-attempt"
  expect_reason 'unexpected activation evidence: recovery-attempt' "${completed[@]}"
  rm -r "$dir/recovery-attempt"
done
for record in schema-job/request schema-job/anchor schema-job/invocation schema-job/allocation writer-job/request writer-job/anchor writer-job/allocation rollback.conf rollback.sha256 previous.endpoint; do
  cp -p "$worker_release/$record" /tmp/changed-record
  printf '\n' >>/tmp/changed-record
  mount --bind /tmp/changed-record "$worker_release/$record"
  expect_reason 'external acceptance differs from recorded proof' "${completed[@]}"
  umount "$worker_release/$record"
  rm /tmp/changed-record
done
touch "$worker_stage_new/activation.confirmation-not-accepted"
refuse_follow 'original recovery windows have no verified terminal acceptance'
rm "$worker_stage_new/activation.confirmation-not-accepted"
printf '[PASS] real supervisor, sealed coordinator, schema/writer job linkage and external API then worker acceptance\n'
python3 -ISB - "$api" "$worker" <<'PY'
import json
from pathlib import Path
import sys
import time

path = Path('/tmp/observations')
state = json.loads(path.read_bytes())
del state['jobs']
for service, digit in zip(sys.argv[1:], 'cd', strict=True):
    live = state['peers'][service]
    loaded = (path.parent / (live[1] + '.loaded')).read_bytes()
    live[:2] = [digit * 32, digit * 64]
    (path.parent / (live[1] + '.loaded')).write_bytes(loaded)
    state[service + '_started'] = time.time_ns()
path.write_text(json.dumps(state))
PY
: >/events
"${completed[@]}" || fail 'terminal recovery acceptance depended on live writer or journal state'
! grep -Eq '^(systemctl|podman|journalctl|busctl|systemd-run) ' /events || { cat /events >&2; fail 'terminal recovery acceptance queried the live host'; }
printf '[PASS] terminal recovery acceptance survives writer restarts and journal loss from persisted receipts\n'
(
  exec 8</opt
  flock -n 8
  exec 7</run/lock/hololive-quadlet-redeploy.lock
  flock -n 7
  refuse_follow 'recovery supervisor lock unavailable'
)
for name in "$api" "$worker"; do
  printf '%s\n' "unhealthy-$name" >/tmp/fault
  refuse_follow "active $name image mismatch"
done
fi
: >/tmp/fault
publish_follow
cmp "$store/active" <(printf '%s\n' "$follow_revision")
cmp "$follow_receipt/revisions" <(printf 'installed=%s\ncandidate=%s\ntool=%s\n' "$candidate" "$follow_revision" "$follow_tool_revision")
cmp "$follow_receipt/pairs.before" "$follow_receipt/pairs.after"
for name in "$api" "$worker"; do
  grep -Fxq "$name $(<"$legacy/artifacts/worker-profile/active/$name.pair")" "$follow_receipt/pairs.after"
done
printf '[PASS] completed receipt admits ordinary two-writer follow-up publication\n'
