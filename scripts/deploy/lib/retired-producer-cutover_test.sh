#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
. "$ROOT_DIR/scripts/deploy/lib/retired-producer-cutover.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fakebin="$tmp/bin"
actions="$tmp/actions"
state_file="$tmp/state"
runtime="$tmp/runtime"
mkdir -p "$fakebin" "$runtime"
: > "$actions"

cat > "$fakebin/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
runtime_dir="${TEST_RUNTIME:?}"
command="$1"
shift
if [[ "${1:-}" == "--now" ]]; then
  shift
fi
if [[ "${1:-}" == "--quiet" ]]; then
  shift
fi
unit="${*: -1}"
unit_file="$runtime_dir/unit.$unit"
enabled_file="$runtime_dir/enabled.$unit"
active_file="$runtime_dir/active.$unit"

case "$command" in
  cat)
    [[ -e "$unit_file" ]]
    ;;
  is-enabled)
    [[ -e "$enabled_file" ]]
    ;;
  is-active)
    [[ -e "$active_file" ]]
    ;;
  enable)
    touch "$enabled_file" "$unit_file"
    printf 'systemctl enable %s\n' "$unit" >> "${TEST_ACTIONS:?}"
    ;;
  start)
    touch "$active_file" "$unit_file"
    printf 'systemctl start %s\n' "$unit" >> "${TEST_ACTIONS:?}"
    ;;
  disable)
    rm -f "$enabled_file" "$active_file"
    printf 'systemctl disable %s\n' "$unit" >> "${TEST_ACTIONS:?}"
    ;;
  stop)
    rm -f "$active_file"
    printf 'systemctl stop %s\n' "$unit" >> "${TEST_ACTIONS:?}"
    ;;
  *)
    exit 1
    ;;
esac
EOF

cat > "$fakebin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
runtime_dir="${TEST_RUNTIME:?}"
case "$1" in
  ps)
    name=""
    for arg in "$@"; do
      case "$arg" in
        name=^*)
          name="${arg#name=^}"
          name="${name%\$}"
          ;;
      esac
    done
    [[ -n "$name" ]] || exit 0
    if [[ "$*" == *'-aq'* || "$*" == *' -a '* ]]; then
      if [[ -e "$runtime_dir/container.$name" ]]; then
        printf '%s-id\n' "$name"
      fi
      exit 0
    fi
    if [[ -e "$runtime_dir/container-active.$name" ]]; then
      printf '%s-id\n' "$name"
    fi
    ;;
  start)
    touch "$runtime_dir/container.$2" "$runtime_dir/container-active.$2"
    printf 'docker start %s\n' "$2" >> "${TEST_ACTIONS:?}"
    ;;
  stop)
    rm -f "$runtime_dir/container-active.$2"
    printf 'docker stop %s\n' "$2" >> "${TEST_ACTIONS:?}"
    ;;
  *)
    exit 1
    ;;
esac
EOF

cat > "$fakebin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" != "-n" ]] || shift
exec "$@"
EOF
chmod +x "$fakebin/systemctl" "$fakebin/docker" "$fakebin/sudo"

export PATH="$fakebin:$PATH"
export TEST_ACTIONS="$actions"
export TEST_RUNTIME="$runtime"
export CONTAINER_CLI=docker

fail() {
  echo "$1" >&2
  exit 1
}

reset_runtime() {
  rm -rf "$runtime"
  mkdir -p "$runtime"
  : > "$actions"
}

producer_unit="hololive-youtube-producer@youtube-collector-a.service"
collector_unit="hololive-youtube-collector@youtube-collector-a.service"
producer_container="hololive-youtube-producer-b"
collector_container="hololive-youtube-collector-b"

producer_active() {
  [[ -e "$runtime/active.$producer_unit" || -e "$runtime/container-active.$producer_container" ]]
}

collector_active() {
  [[ -e "$runtime/active.$collector_unit" || -e "$runtime/container-active.$collector_container" ]]
}

assert_not_both() {
  if producer_active && collector_active; then
    fail "producer and collector were both active ($1)"
  fi
}

seed_compose_producer() {
  touch "$runtime/container.$producer_container" "$runtime/container-active.$producer_container"
}

seed_native_producer() {
  touch "$runtime/unit.$producer_unit" "$runtime/enabled.$producer_unit" "$runtime/active.$producer_unit"
}

start_compose_collector() {
  touch "$runtime/container.$collector_container" "$runtime/container-active.$collector_container"
}

stop_compose_collector() {
  rm -f "$runtime/container-active.$collector_container"
}

start_native_collector() {
  touch "$runtime/unit.$collector_unit" "$runtime/enabled.$collector_unit" "$runtime/active.$collector_unit"
}

stop_native_collector() {
  rm -f "$runtime/enabled.$collector_unit" "$runtime/active.$collector_unit"
}

reset_runtime
seed_native_producer
seed_compose_producer
write_retired_producer_runtime_state youtube-collector-a > "$state_file"
grep -qx "unit-enabled $producer_unit" "$state_file" || fail "state missing unit-enabled"
grep -qx "unit-active $producer_unit" "$state_file" || fail "state missing unit-active"
grep -qx "container-active $producer_container" "$state_file" || fail "state missing container-active"

reset_runtime
seed_native_producer
seed_compose_producer
write_retired_producer_runtime_state youtube-collector-a > "$state_file"
stop_retired_producer_runtime youtube-collector-a
start_native_collector
start_compose_collector
producer_active && fail "compose/native first cutover left a producer running"
collector_active || fail "first cutover did not start collector"
assert_not_both "first cutover"

if require_named_containers_inactive "$collector_container"; then
  fail "collector inactivity check accepted a running collector container"
fi
if require_named_units_inactive "$collector_unit"; then
  fail "collector inactivity check accepted a running collector unit"
fi
assert_not_both "blocked restore while collector still running"
stop_named_units_and_require_inactive "$collector_unit"
stop_named_containers_and_require_inactive "$collector_container"
restore_retired_producer_runtime "$state_file" youtube-collector-a
producer_active || fail "first-cutover rollback did not restore producer"
collector_active && fail "first-cutover rollback left collector running"
assert_not_both "first-cutover rollback"
grep -qx "systemctl enable $producer_unit" "$actions" || fail "rollback did not re-enable producer unit"
grep -qx "systemctl start $producer_unit" "$actions" || fail "rollback did not start producer unit"
grep -qx "docker start $producer_container" "$actions" || fail "rollback did not start producer container"
if grep -q 'producer-a' "$actions"; then
  fail "restore started an unrecorded producer runtime"
fi

reset_runtime
seed_native_producer
write_retired_producer_runtime_state youtube-collector-a > "$state_file"
stop_retired_producer_runtime youtube-collector-a
start_native_collector
if producer_active && collector_active; then
  fail "failed-cutover window allowed both runtimes"
fi
stop_native_collector
restore_retired_producer_runtime "$state_file" youtube-collector-a
collector_active && fail "failed cutover restore left collector running"
producer_active || fail "failed cutover did not restore producer"
assert_not_both "failed-cutover restore"

printf 'unit-active attacker.service\n' > "$state_file"
if validate_retired_producer_runtime_state "$state_file" youtube-collector-a >/dev/null 2>&1; then
  fail "state validation accepted an unapproved unit"
fi

echo "retired producer cutover state checks passed"
