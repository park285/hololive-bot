#!/usr/bin/env bash
set -Eeuo pipefail

die() { echo "[pg-major-upgrade] $*" >&2; exit 1; }
log() { echo "[pg-major-upgrade] $*"; }
usage() { echo "Usage: $0 [--check]" >&2; exit 2; }

check_only=false
case "$#:${1:-}" in
  0:) ;;
  1:--check) check_only=true ;;
  *) usage ;;
esac

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  [[ -z "${HOLOLIVE_QUADLET_TEST_ROOT:-}" ]] || die "HOLOLIVE_QUADLET_TEST_ROOT is only for non-root tests"
  prefix=""
  [[ "$ROOT_DIR" == /opt/hololive-bot/compose/current ]] || die "run the installed copy under /opt/hololive-bot/compose/current"
else
  prefix="${HOLOLIVE_QUADLET_TEST_ROOT:-}"
  [[ -n "$prefix" ]] || die "must run as root (sudo)"
fi
quadlet_dir="$prefix/etc/containers/systemd/hololive-bot"
compose_env="$prefix/etc/stack-secrets/hololive-bot/compose.env"
installed_prestart="$prefix/usr/local/libexec/hololive-bot/quadlet-prestart"
state_dir="$prefix/var/lib/hololive-bot"
marker="$state_dir/.pg-major.pending"
fingerprint_sql="$ROOT_DIR/scripts/maintenance/pg-major-fingerprint.sql"
globals_sql="$ROOT_DIR/scripts/maintenance/pg-major-globals.sql"
clear_default_acl_sql="$ROOT_DIR/scripts/maintenance/pg-major-clear-default-acl.sql"
quiet_logs='-c log_statement=none -c log_min_duration_statement=-1 -c log_min_error_statement=panic -c log_min_messages=panic -c pg_stat_statements.track=none -c client_min_messages=warning'

# shellcheck source=scripts/deploy/lib/compose-env.sh
. "$ROOT_DIR/scripts/deploy/lib/compose-env.sh"
# shellcheck source=scripts/deploy/lib/collector-slots.sh
. "$ROOT_DIR/scripts/deploy/lib/collector-slots.sh"

env_value() {
  if compose_env_key_exists_in_file "$compose_env" "$1"; then
    compose_env_read_value_from_file "$compose_env" "$1"
  else
    printf '%s\n' "$2"
  fi
}

image_major() {
  podman image inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$1" | sed -n 's/^PG_MAJOR=//p'
}

data_volume() {
  podman container inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql"}}{{.Name}}{{end}}{{end}}' holo-postgres
}

volume_state() {
  local status=0
  podman volume exists "$1" || status=$?
  case "$status" in
    0) echo present ;;
    1) echo absent ;;
    *) return 1 ;;
  esac
}

psql_admin() {
  podman exec -i ${PGOPTIONS:+-e PGOPTIONS} holo-postgres psql \
    --username "$admin_user" \
    --dbname hololive \
    --no-psqlrc \
    --set=ON_ERROR_STOP=1 \
    --set=VERBOSITY=terse \
    --set=SHOW_CONTEXT=never \
    --tuples-only \
    --no-align \
    --quiet "$@"
}

scalar() {
  psql_admin <<<"$1"
}

wait_final_server() {
  local _
  for _ in $(seq 1 600); do
    # shellcheck disable=SC2016
    if podman exec holo-postgres sh -c 'test "$(cat /proc/1/comm)" = postgres && pg_isready -q -p 5432' >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

clear_default_privileges() {
  psql_admin <"$clear_default_acl_sql"
}

write_marker() {
  local temporary
  temporary="$(mktemp "$marker.XXXXXX")"
  printf 'phase=%s\ndir=%s\nsource_volume=%s\nsource_image=%s\ntarget_volume=%s\ntarget_image=%s\nwriters=%s\n' \
    "$1" "$work_dir" "$source_volume" "$source_image" "$target_volume" "$target_image" "${active_units[*]}" >"$temporary"
  chmod 0600 -- "$temporary"
  mv -f -- "$temporary" "$marker"
}

start_units() {
  local unit
  for unit in holo-pgbouncer.service "${writer_units[@]}"; do
    if [[ " ${active_units[*]} " == *" $unit "* ]]; then
      systemctl start "$unit"
    fi
  done
}

admin_user="$(env_value POSTGRES_ADMIN_USER postgres_admin)"
central_slot="$(collector_slots_where host central)" && [[ "$central_slot" =~ ^[a-z]$ ]] || die "central collector slot is missing"
central_collector="$(collector_slot "$central_slot" collector_unit)" || die "central collector unit is missing"
writer_units=("$central_collector" hololive-alarm-worker.service hololive-api.service)
stop_units=(hololive-api.service hololive-alarm-worker.service "$central_collector" holo-pgbouncer.service)
bootstrap_roles="$admin_user,hololive_runtime,hololive_alarm_worker,$(env_value HOLOLIVE_MIGRATOR_USER hololive_migrator),hololive_scraper"
bootstrap_roles+=",hololive_collector_a,hololive_collector_b,hololive_collector_c,hololive_collector_d"
if [[ -n "$(env_value HOLOLIVE_REPLICATOR_PASSWORD '')" ]]; then
  bootstrap_roles+=",$(env_value HOLOLIVE_REPLICATOR_USER hololive_replicator)"
fi

[[ ! -e "$marker" && ! -L "$marker" ]] || die "unresolved PostgreSQL major upgrade recorded in $marker; inspect it first"
for unit_file in holo-postgres.container holo-pg-data.volume; do
  cmp -s -- "$quadlet_dir/$unit_file" "$ROOT_DIR/deploy/quadlet/hololive-bot/$unit_file" ||
    die "installed $unit_file differs from this revision; install this revision with sync-opt-current.sh first"
done
cmp -s -- "$installed_prestart" "$ROOT_DIR/scripts/deploy/quadlet-prestart.sh" ||
  die "installed quadlet-prestart differs from this revision; install this revision with sync-opt-current.sh first"
systemctl is-active --quiet holo-postgres.service || die "holo-postgres.service must be active"
[[ "$(systemctl show hololive-db-migrate.service -p ActiveState --value)" != active ]] || die "hololive-db-migrate.service is running"
target_volume="$(sed -n 's/^VolumeName=//p' "$quadlet_dir/holo-pg-data.volume")"
[[ -n "$target_volume" ]] || die "VolumeName is missing from $quadlet_dir/holo-pg-data.volume"
source_volume="$(data_volume)"
[[ -n "$source_volume" && "$source_volume" != "$target_volume" ]] ||
  die "running holo-postgres must use a volume other than $target_volume; observed: ${source_volume:-none}"
[[ "$(volume_state "$target_volume" || true)" == absent ]] ||
  die "target volume $target_volume already exists or cannot be confirmed; inspect it before an upgrade"
source_image="$(podman container inspect --format '{{.Image}}' holo-postgres)"
source_image="sha256:${source_image#sha256:}"
[[ "$source_image" =~ ^sha256:[0-9a-f]{64}$ ]] || die "running holo-postgres image ID is unknown"
target_image="$(env_value POSTGRES_IMAGE '')"
[[ "$target_image" =~ ^sha256:[0-9a-f]{64}$ ]] || die "compose.env POSTGRES_IMAGE must be the loaded PostgreSQL 19 image ID (sha256:<id>)"
[[ "sha256:$(podman image inspect --format '{{.Id}}' "$target_image")" == "$target_image" ]] ||
  die "configured PostgreSQL image $target_image is not loaded"
[[ "$(image_major "$source_image")" == 18 ]] || die "running holo-postgres image is not PostgreSQL 18"
[[ "$(image_major "$target_image")" == 19 ]] || die "configured PostgreSQL image is not PostgreSQL 19"
[[ "$(podman image inspect --format '{{.Architecture}}' "$source_image")" == "$(podman image inspect --format '{{.Architecture}}' "$target_image")" ]] ||
  die "configured PostgreSQL image architecture differs from the running one"
version="$(scalar 'SHOW server_version_num;')"
[[ "$version" =~ ^18[0-9]{4}$ ]] || die "source cluster must be PostgreSQL 18; observed server_version_num=${version:-unknown}"
size="$(scalar "SELECT pg_database_size('hololive');")"
connection_limit="$(scalar "SELECT datconnlimit FROM pg_database WHERE datname = 'hololive';")"
[[ "$size" =~ ^[0-9]+$ && "$connection_limit" =~ ^-?[0-9]+$ ]] || die "cannot read the database size and connection limit"
install -d -m 0700 -- "$state_dir"
graph_root="$(podman info --format '{{.Store.GraphRoot}}')"
for path in "$state_dir" "$graph_root"; do
  avail="$(df --output=avail -B1 -- "$path" | tail -n 1 | tr -d ' ')"
  if [[ ! "$avail" =~ ^[0-9]+$ ]] || (( avail < size * 3 )); then
    die "free space ${avail:-unknown} bytes under $path is below three times the database size $size"
  fi
done
PGOPTIONS="$quiet_logs" psql_admin --set=bootstrap_roles="$bootstrap_roles" <"$globals_sql" >/dev/null ||
  die "cluster roles cannot be carried to the new cluster"
active_units=()
for unit in "${stop_units[@]}"; do
  if systemctl is-active --quiet "$unit"; then active_units+=("$unit"); fi
done
log "preflight passed: server_version_num=$version, $source_volume -> $target_volume, $source_image -> $target_image, database $size bytes, running: ${active_units[*]:-none}"
if [[ "$check_only" == true ]]; then
  exit 0
fi

work_dir="$state_dir/pg-major/$(date -u +%Y%m%dT%H%M%SZ)"
install -d -m 0700 -- "$state_dir/pg-major" "$work_dir"
source_stopped=false
committed=false
abort() {
  local status=$?
  [[ "$committed" == false ]] || return 0
  set +e
  if [[ "$source_stopped" == false ]]; then
    rm -f -- "$marker"
    start_units
    echo "[pg-major-upgrade] aborted before PostgreSQL 18 stopped; it keeps serving and the stopped units were started again. Files: $work_dir" >&2
    exit "$status"
  fi
  systemctl stop holo-postgres.service
  cat >&2 <<EOF
[pg-major-upgrade] aborted after PostgreSQL 18 stopped. holo-postgres, holo-pgbouncer and the writers stay stopped while $marker exists.
[pg-major-upgrade] The previous volume $source_volume was only read. The dump and fingerprints are in $work_dir.
[pg-major-upgrade] Return to PostgreSQL 18: set POSTGRES_IMAGE=$source_image in the compose.env master and sync it (stack-platform-ops),
[pg-major-upgrade] install the previous revision's holo-postgres.container and holo-pg-data.volume into $quadlet_dir and run systemctl daemon-reload,
[pg-major-upgrade] remove $marker, then start holo-postgres.service and: ${active_units[*]:-none}.
[pg-major-upgrade] To retry instead, return to PostgreSQL 18 first, then inspect and remove the volume $target_volume.
EOF
  exit "$status"
}
trap abort EXIT

write_marker dump
log "stopping writers and the pooler: ${stop_units[*]}"
systemctl stop "${stop_units[@]}"
remaining=
for _ in $(seq 1 30); do
  remaining="$(scalar "SELECT count(*) FROM pg_stat_activity WHERE backend_type = 'client backend' AND pid <> pg_backend_pid() AND usename NOT IN ('$admin_user', 'postgres_exporter', 'iris_db_nightly');")"
  [[ "$remaining" != 0 ]] || break
  sleep 2
done
[[ "$remaining" == 0 ]] || die "$remaining client sessions remain after stopping the writers"
log "dump to $work_dir"
(
  umask 077
  podman exec holo-postgres pg_dump --username "$admin_user" --format=custom --dbname hololive >"$work_dir/hololive.dump"
  PGOPTIONS="$quiet_logs" psql_admin --set=bootstrap_roles="$bootstrap_roles" <"$globals_sql" >"$work_dir/globals.sql"
  psql_admin <"$fingerprint_sql" >"$work_dir/fingerprint.source"
)
source_stopped=true
systemctl stop holo-postgres.service
if podman container exists holo-postgres &&
  [[ "$(podman container inspect --format '{{.State.Status}}' holo-postgres)" == running ]]; then
  die "PostgreSQL 18 is still running after stop"
fi
write_marker restore
log "starting PostgreSQL 19 on $target_volume"
systemctl start holo-postgres.service
wait_final_server || die "PostgreSQL 19 did not finish initialization within 600 seconds"
[[ "$(data_volume)" == "$target_volume" ]] || die "new holo-postgres does not use $target_volume"
running_image="$(podman container inspect --format '{{.Image}}' holo-postgres)"
[[ "sha256:${running_image#sha256:}" == "$target_image" ]] || die "new holo-postgres does not run $target_image"
version="$(scalar 'SHOW server_version_num;')"
[[ "$version" =~ ^19[0-9]{4}$ ]] || die "new cluster is not PostgreSQL 19; observed server_version_num=${version:-unknown}"
scalar "ALTER DATABASE hololive CONNECTION LIMIT 0;" >/dev/null
scalar "SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE datname = 'hololive' AND pid <> pg_backend_pid() AND usesysid NOT IN (SELECT oid FROM pg_roles WHERE rolsuper);" >/dev/null
log "carrying roles and settings, then restoring"
PGOPTIONS="$quiet_logs" psql_admin --single-transaction <"$work_dir/globals.sql" >/dev/null ||
  die "carrying cluster roles to the new cluster failed"
[[ "$(clear_default_privileges)" == 0 ]] ||
  die "cannot clear the new cluster's default privileges; restored objects would gain grants the source does not have"
podman exec -i holo-postgres pg_restore --username "$admin_user" --dbname hololive --single-transaction --exit-on-error <"$work_dir/hololive.dump"
(
  umask 077
  psql_admin <"$fingerprint_sql" >"$work_dir/fingerprint.target"
)
if ! cmp -s -- "$work_dir/fingerprint.source" "$work_dir/fingerprint.target"; then
  diff -- "$work_dir/fingerprint.source" "$work_dir/fingerprint.target" | head -n 40 >&2 || true
  die "restored cluster differs from the source fingerprint"
fi
log "restored cluster matches the source fingerprint; analyzing"
scalar 'ANALYZE;' >/dev/null
scalar "ALTER DATABASE hololive CONNECTION LIMIT $connection_limit;" >/dev/null
rm -f -- "$marker"
committed=true
start_units
log "PostgreSQL major upgrade committed; the previous volume $source_volume and $work_dir are kept"
