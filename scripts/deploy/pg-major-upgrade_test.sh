#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
if [[ "$EUID" -eq 0 ]]; then
  echo 'run this fixture as a non-root user' >&2
  exit 1
fi
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
source_id="$(printf 'a%.0s' {1..64})"
target_id="$(printf 'b%.0s' {1..64})"

new_host() {
  T="$fixture/$1"
  mkdir -p "$T/etc/containers/systemd/hololive-bot" "$T/etc/stack-secrets/hololive-bot" \
    "$T/usr/local/libexec/hololive-bot" "$T/var/lib" "$T/bin" "$T/state/active" "$T/volumes" "$T/graph"
  cp "$root/deploy/quadlet/hololive-bot/holo-postgres.container" "$root/deploy/quadlet/hololive-bot/holo-pg-data.volume" \
    "$T/etc/containers/systemd/hololive-bot/"
  cp "$root/scripts/deploy/quadlet-prestart.sh" "$T/usr/local/libexec/hololive-bot/quadlet-prestart"
  printf 'POSTGRES_IMAGE=sha256:%s\nHOLOLIVE_REPLICATOR_PASSWORD=x\n' "$target_id" >"$T/etc/stack-secrets/hololive-bot/compose.env"
  for unit in holo-postgres hololive-api hololive-alarm-worker hololive-collector-c; do
    touch "$T/state/active/$unit.service"
  done
  touch "$T/volumes/hololive-bot_holo-pg-data"
  printf 'hololive-bot_holo-pg-data\n' >"$T/state/volume"
  printf 'sha256:%s\n' "$source_id" >"$T/state/image"
  printf '180007\n' >"$T/state/version"
  printf 'rows|public|t|3|42\n' >"$T/state/rows"
  cat >"$T/bin/systemctl" <<'SH'
#!/usr/bin/env bash
S="$HOLOLIVE_QUADLET_TEST_ROOT/state"
printf 'systemctl %s\n' "$*" >>"$S/calls"
case "$1" in
  is-active) [[ -e "$S/active/$3" ]] ;;
  show) echo inactive ;;
  stop) shift; for unit in "$@"; do rm -f "$S/active/$unit"; done ;;
  start)
    [[ ! -e "$S/fail-start-$2" ]] || exit 1
    touch "$S/active/$2"
    if [[ "$2" == holo-postgres.service ]]; then
      sed -n 's/^VolumeName=//p' "$HOLOLIVE_QUADLET_TEST_ROOT/etc/containers/systemd/hololive-bot/holo-pg-data.volume" >"$S/volume"
      touch "$HOLOLIVE_QUADLET_TEST_ROOT/volumes/$(cat "$S/volume")"
      sed -n 's/^POSTGRES_IMAGE=//p' "$HOLOLIVE_QUADLET_TEST_ROOT/etc/stack-secrets/hololive-bot/compose.env" >"$S/image"
      printf '190000\n' >"$S/version"
      [[ ! -e "$S/target-rows" ]] || cp "$S/target-rows" "$S/rows"
    fi ;;
  *) exit 2 ;;
esac
SH
  cat >"$T/bin/podman" <<'SH'
#!/usr/bin/env bash
S="$HOLOLIVE_QUADLET_TEST_ROOT/state"
major() { case "$1" in *aaaa*) echo 18 ;; *bbbb*) [[ -e "$S/target-major-18" ]] && echo 18 || echo 19 ;; esac; }
case "$1 $2" in
  "container inspect")
    case "$4" in
      *Mounts*) cat "$S/volume" ;;
      '{{.Image}}') sed 's/^sha256://' "$S/image" ;;
      *State.Status*) [[ -e "$S/active/holo-postgres.service" ]] && echo running || echo exited ;;
    esac ;;
  "container exists") exit 0 ;;
  "volume exists") [[ -e "$HOLOLIVE_QUADLET_TEST_ROOT/volumes/$3" ]] || exit 1 ;;
  "image inspect")
    case "$4" in
      '{{.Id}}') echo "${5#sha256:}" ;;
      *Env*) printf 'PATH=/usr/bin\nPG_MAJOR=%s\n' "$(major "$5")" ;;
      *Architecture*) echo arm64 ;;
    esac ;;
  "info --format") echo "$HOLOLIVE_QUADLET_TEST_ROOT/graph" ;;
  exec\ *)
    [[ -e "$S/active/holo-postgres.service" ]] || exit 125
    case "$*" in
      *"/proc/1/comm"*)
        polls="$(cat "$S/init-polls" 2>/dev/null || echo 0)"
        echo $((polls + 1)) >"$S/init-polls"
        if [[ "$(cat "$S/version")" == 19* ]] && (( polls < 2 )); then exit 1; fi
        echo final-server >>"$S/calls" ;;
      *pg_dump*) echo pg_dump >>"$S/calls"
        [[ ! -e "$S/fail-dump" ]] || exit 1; echo dump ;;
      *pg_restore*) echo pg_restore >>"$S/calls"; cat >/dev/null ;;
      *psql*)
        sql="$(cat)"
        case "$sql" in
          *server_version_num*) cat "$S/version" ;;
          *pg_database_size*) echo 1000 ;;
          *datconnlimit\ FROM*) echo -1 ;;
          *bootstrap_roles*) echo "CREATE ROLE iris_db_nightly;" ;;
          *"'rows'"*) printf 'role|postgres_admin\n'; cat "$S/rows" ;;
          *"usename LIKE"*) cat "$S/remote-collectors" 2>/dev/null || echo 0 ;;
          *pg_stat_activity*backend_type*) echo 0 ;;
          *"count(*) FROM pg_catalog.pg_default_acl"*) echo default-acl >>"$S/calls"; echo 0 ;;
          *) printf 'psql %s\n' "$(head -c 60 <<<"$sql")" >>"$S/calls" ;;
        esac ;;
    esac ;;
  *) exit 2 ;;
esac
SH
  chmod +x "$T/bin/"*
}

run_tool() {
  PATH="$T/bin:$PATH" HOLOLIVE_QUADLET_TEST_ROOT="$T" bash "$root/scripts/deploy/pg-major-upgrade.sh" "$@"
}

expect_refusal() {
  local message="$1"
  shift
  if run_tool "$@" >"$T/out" 2>&1; then
    echo "pg-major-upgrade accepted: $message" >&2
    exit 1
  fi
  grep -qF -- "$message" "$T/out" || { cat "$T/out" >&2; exit 1; }
}

marker() { printf '%s\n' "$T/var/lib/hololive-bot/.pg-major.pending"; }
line_of() { grep -nF -- "$1" "$T/state/calls" | head -n 1 | cut -d: -f1; }

new_host check
run_tool --check >"$T/out"
grep -qF 'preflight passed' "$T/out"
[[ ! -e "$(marker)" ]]
if grep -q 'systemctl stop' "$T/state/calls"; then exit 1; fi
echo 'check mode passes preflight without side effects'

new_host refusals
mkdir -p "$T/var/lib/hololive-bot" && touch "$(marker)"
expect_refusal 'unresolved PostgreSQL major upgrade'
rm "$(marker)"
touch "$T/volumes/hololive-bot_holo-pg-data19"
expect_refusal 'already exists or cannot be confirmed'
rm "$T/volumes/hololive-bot_holo-pg-data19"
touch "$T/state/target-major-18"
expect_refusal 'configured PostgreSQL image is not PostgreSQL 19'
rm "$T/state/target-major-18"
printf '# drift\n' >>"$T/etc/containers/systemd/hololive-bot/holo-postgres.container"
expect_refusal 'installed holo-postgres.container differs'
new_host remote-collectors
printf '2\n' >"$T/state/remote-collectors"
expect_refusal 'AP collectors still hold 2 sessions'
if grep -q 'systemctl stop' "$T/state/calls"; then exit 1; fi
echo 'preflight refuses unresolved markers, existing targets, wrong majors, stale units and connected AP collectors before stopping anything'

new_host commit
run_tool >"$T/out"
grep -qF 'major upgrade committed' "$T/out"
[[ ! -e "$(marker)" ]]
(( $(line_of 'systemctl stop hololive-api.service') < $(line_of 'pg_dump') ))
(( $(line_of 'pg_dump') < $(line_of 'systemctl stop holo-postgres.service') ))
(( $(line_of 'systemctl start holo-postgres.service') < $(line_of 'final-server') ))
(( $(line_of 'final-server') < $(line_of 'CONNECTION LIMIT 0') ))
(( $(line_of 'CONNECTION LIMIT 0') < $(line_of 'default-acl') ))
(( $(line_of 'default-acl') < $(line_of 'pg_restore') ))
(( $(line_of 'pg_restore') < $(line_of 'systemctl start hololive-collector-c.service') ))
(( $(line_of 'systemctl start hololive-collector-c.service') < $(line_of 'systemctl start hololive-api.service') ))
grep -qF 'CONNECTION LIMIT -1' "$T/state/calls"
[[ "$(cat "$T/state/volume")" == hololive-bot_holo-pg-data19 ]]
work="$(find "$T/var/lib/hololive-bot/pg-major" -mindepth 1 -maxdepth 1 -type d)"
[[ "$(stat -c %a "$work")" == 700 && -s "$work/hololive.dump" && -s "$work/globals.sql" ]]
cmp -s "$work/fingerprint.source" "$work/fingerprint.target"
echo 'commit stops writers before the dump, waits for the final PostgreSQL 19 server after init, gates restore behind a zero connection limit, clears preset default privileges before restoring and restarts writers last'

new_host mismatch
printf 'rows|public|t|2|41\n' >"$T/state/target-rows"
expect_refusal 'differs from the source fingerprint'
grep -qx 'phase=restore' "$(marker)"
[[ ! -e "$T/state/active/holo-postgres.service" && ! -e "$T/state/active/hololive-api.service" ]]
if grep -qF 'systemctl start hololive-api.service' "$T/state/calls"; then exit 1; fi
grep -qF 'POSTGRES_IMAGE=sha256:'"$source_id" "$T/out"
echo 'a fingerprint mismatch leaves PostgreSQL and the writers stopped behind the marker with recovery steps'

new_host early
touch "$T/state/fail-dump"
expect_refusal 'aborted before PostgreSQL 18 stopped'
[[ ! -e "$(marker)" ]]
if grep -qF 'systemctl stop holo-postgres.service' "$T/state/calls"; then exit 1; fi
for unit in hololive-collector-c hololive-alarm-worker hololive-api; do
  [[ -e "$T/state/active/$unit.service" ]]
done
echo 'a failure before PostgreSQL 18 stops clears the marker and restarts the stopped units'

prestart() {
  local volume_root="$fixture/prestart-volumes"
  mkdir -p "$T/units" "$volume_root/hololive-bot_holo-pg-data19" "$T/bin"
  cp "$root/deploy/quadlet/hololive-bot/holo-pg-data.volume" "$T/units/"
  cat >"$T/bin/podman" <<SH
#!/usr/bin/env bash
case "\$1 \$2" in
  "volume inspect") echo "$volume_root/\$5" ;;
  "volume ls") ls "$volume_root" ;;
  *) exit 2 ;;
esac
SH
  chmod +x "$T/bin/podman"
  : >"$T/compose.env"
  PATH="$T/bin:$PATH" HOLOLIVE_QUADLET_TEST_ROOT="$T" bash "$root/scripts/deploy/quadlet-prestart.sh" "$1" >"$T/out" 2>&1 || true
}
T="$fixture/prestart"
mkdir -p "$T/var/lib/hololive-bot"
passed_guard() { grep -qF 'compose env file is invalid' "$T/out"; }
prestart holo-postgres
passed_guard
mkdir -p "$fixture/prestart-volumes/hololive-bot_holo-pg-data"
prestart holo-postgres
grep -qF 'holds a previous one' "$T/out"
mkdir -p "$fixture/prestart-volumes/hololive-bot_holo-pg-data19/pgdata"
touch "$fixture/prestart-volumes/hololive-bot_holo-pg-data19/pgdata/PG_VERSION"
prestart holo-postgres
passed_guard
rm "$fixture/prestart-volumes/hololive-bot_holo-pg-data19/pgdata/PG_VERSION"
printf 'phase=dump\n' >"$(marker)"
prestart holo-postgres
grep -qF 'holo-postgres stays stopped' "$T/out"
printf 'phase=restore\n' >"$(marker)"
prestart holo-postgres
passed_guard
echo 'prestart keeps PostgreSQL down during an upgrade and refuses an empty cluster beside a previous one'
