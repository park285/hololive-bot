#!/usr/bin/env bash
set -Eeuo pipefail
[[ $# -eq 6 ]] || exit 2
slot="$1" mode="$2" staging="$3" backup="$4" revision="$5" version="$6"
case "$slot" in
  c) remote_root=/opt/hololive-bot/compose service=youtube-collector port=30025 host_label=central ;;
  b) remote_root=/opt/hololive-bot/collector-b service=youtube-collector-b port=30015 host_label=iris-seoul ;;
  *) exit 2 ;;
esac
[[ "$mode" == deploy || "$mode" == rollback || "$mode" == check ]] || exit 2
[[ "$staging" =~ ^$remote_root/po-staging-[0-9]{8}T[0-9]{6}Z$ ]] || exit 2
[[ "$backup" =~ ^$remote_root/backups/po-$slot-[0-9]{8}T[0-9]{6}Z$ ]] || exit 2
[[ "$revision" =~ ^[0-9a-f]{40}$ && "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 2
install -d -m 0755 /run/lock
lock_name=hololive-quadlet-redeploy
[[ "$slot" != b ]] || lock_name=hololive-collector-b-cutover
exec 9>"/run/lock/$lock_name.lock"
flock -n 9 || { echo 'another Quadlet writer is running' >&2; exit 1; }
current="$remote_root/current"
unit_dir=/etc/containers/systemd/hololive-bot
manifest="../po-current-$slot/rootfs-manifest.json"
image_id_file="../po-current-$slot/image-id"
change_id="${backup##*/po-"$slot"-}"
profile_release="$current/artifacts/worker-profile/releases/po-$slot-$change_id/$service"
collector=localhost/hololive-youtube-collector:prod
issuer=localhost/hololive-youtube-po-sandbox:prod
collector_candidate=localhost/hololive-youtube-collector:candidate
issuer_candidate=localhost/hololive-youtube-po-sandbox:candidate
collector_rollback="localhost/hololive-youtube-collector:rollback-$change_id"
issuer_rollback="localhost/hololive-youtube-po-sandbox:rollback-$change_id"
collector_container="hololive-youtube-collector-$slot"
issuer_container="hololive-youtube-po-$slot"
collector_unit="$collector_container.service"
issuer_unit="$issuer_container.service"
[[ "$(stat -c '%u:%g %a' "$staging")" == '0:0 700' ]] || { echo "$host_label staged artifacts are not root-private" >&2; exit 1; }
cd "$current"
. "$staging/scripts/deploy/lib/po-sandbox-image.sh"
pair_tool="$staging/scripts/deploy/lib/worker-profile-pair.py"
cutover_started=false

image_id() {
  local id
  id="$(podman image inspect -f '{{.Id}}' "$1")" || return
  printf 'sha256:%s\n' "$id"
}
container_image_id() {
  local id
  id="$(podman inspect -f '{{.Image}}' "$1")" || return
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
  local started="$1" socket_mount running expected
  systemctl is-active --quiet "$issuer_unit"
  [[ "$(podman inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$issuer_container")" == healthy ]]
  [[ "$(podman inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$issuer_container")" == "$started" ]]
  running="$(container_image_id "$issuer_container")" || return 1
  expected="$(image_id "$issuer")" || return 1
  [[ "$running" == "$expected" ]]
  socket_mount="$(podman inspect -f '{{range .Mounts}}{{if eq .Destination "/run/hololive-youtube-po"}}{{.Source}}{{end}}{{end}}' "$issuer_container")"
  [[ "$(stat -c '%u:%g %a' "$socket_mount")" == '65532:1000 770' ]]
  podman exec "$issuer_container" /app/bin/po-broker --healthcheck --socket /run/hololive-youtube-po/worker.sock
}
verify_collector_runtime() {
  local expected="$1" running tagged
  systemctl is-active --quiet "$collector_unit"
  [[ "$(podman inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$collector_container")" == healthy ]]
  [[ "$(podman inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$collector_container")" == "$expected" ]]
  running="$(container_image_id "$collector_container")" || return 1
  tagged="$(image_id "$collector")" || return 1
  [[ "$running" == "$tagged" ]]
  podman exec "$collector_container" ./bin/healthcheck "https://127.0.0.1:$port/ready" >/dev/null
}
started_since() {
  local since_epoch="$1" container="$2"
  [[ "$(podman inspect -f '{{.State.StartedAt.Unix}}' "$container")" -ge "$since_epoch" ]]
}
require_post_cutover_logs() {
  local since="$1" container="$2" output status
  output="$(podman logs --since "$since" "$container" 2>&1)" || {
    status="$?"
    echo "failed to read post-cutover logs for $container" >&2
    return "$status"
  }
  if grep -E 'ERR|panic|permission denied|x509|no such file|OOM' <<<"$output"; then
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
  cp -p -- "$installed" "$profile_release/rollback-replaced-$collector_container.container" || return
  tmp="$(mktemp "$unit_dir/.$collector_container.container.XXXXXX")" || return
  if ! install -m 0644 -- "$recorded" "$tmp" || ! mv -f -- "$tmp" "$installed"; then
    rm -f -- "$tmp"
    return 1
  fi
  echo "[ROLLBACK] restored the collector base unit the previous image ran with; reinstall the intended revision's unit before the next deployment" >&2
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
restore() {
  local old_revision old_po_revision state issuer_now
  trap - ERR INT TERM HUP
  [[ -r "$backup/snapshot-complete" ]] || { echo "$host_label rollback snapshot incomplete" >&2; return 1; }
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
  else
    [[ "$state" == absent ]] || return 1
  fi
  python3 "$pair_tool" verify "$service" "$profile_release/previous" "$collector_rollback"
  python3 "$pair_tool" check "$service" "$profile_release/previous"
  python3 "$pair_tool" activate "$service" "$profile_release/previous" "$current" || return
  restore_collector_base_unit || return
  podman tag "$collector_rollback" "$collector"
  systemctl daemon-reload
  if [[ "$state" == present ]]; then
    podman tag "$issuer_rollback" "$issuer"
    install -D -m 0644 "$backup/po-prechange-manifest.json" "$current/$manifest"
    install -D -m 0644 "$backup/po-prechange.image-id" "$current/$image_id_file"
    systemctl restart "$issuer_unit"
    verify_issuer_runtime "$old_po_revision"
  else
    systemctl stop "$issuer_unit"
    rm -f "$current/$manifest" "$current/$image_id_file"
    issuer_now="$(issuer_presence)" || return 1
    if [[ "$issuer_now" == present ]]; then podman image rm "$issuer" >/dev/null; fi
  fi
  systemctl restart "$collector_unit" || return
  verify_collector_runtime "$old_revision" || return
  echo "$host_label previous collector/issuer restored from $backup"
}
require_collector_lease_migration() {
  local migration=001_schema_epoch3_baseline.sql expected observed collector_env
  local migrator_user postgres_image owner leftover status=0
  expected="$(sha256sum "$staging/hololive/hololive-api/scripts/migrations/$migration")"
  expected="${expected%% *}"
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || return 1
  collector_env="$(podman inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$collector_container")" || return 1
  if ! grep -qx 'POSTGRES_HOST=holo-postgres' <<<"$collector_env" || ! grep -qx 'POSTGRES_PORT=5432' <<<"$collector_env" \
      || ! grep -qx 'POSTGRES_DB=hololive' <<<"$collector_env"; then
    echo 'collector migration proof requires the local primary route' >&2
    return 1
  fi
  migrator_user="$(awk -F= '$1 == "HOLOLIVE_MIGRATOR_USER" { v = substr($0, length($1) + 2); gsub(/^["\047]|["\047]$/, "", v); print v }' \
    /etc/stack-secrets/hololive-bot/compose.env)"
  migrator_user="${migrator_user:-hololive_migrator}"
  [[ "$migrator_user" =~ ^[a-z_][a-z0-9_]*$ ]] || { echo 'collector migration proof requires a valid migrator role' >&2; return 1; }
  postgres_image="$(podman inspect -f '{{.Image}}' holo-postgres)" || return 1
  [[ "$postgres_image" =~ ^[0-9a-f]{64}$ ]] || { echo 'collector migration proof client identity is invalid' >&2; return 1; }
  owner="hololive.collector-migration-proof=$(cat /proc/sys/kernel/random/uuid)"
  observed="$(timeout 20 podman run --rm --label "$owner" --pull=never --read-only --cap-drop=all \
    --security-opt=no-new-privileges --network none \
    --env PGHOST=/var/run/postgresql --env PGPORT=5432 --env PGDATABASE=hololive --env "PGUSER=$migrator_user" \
    --env PGSSLMODE=disable --env PGCONNECT_TIMEOUT=5 --env PGAPPNAME=hololive-collector-migration-proof \
    --env 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=5s -c lock_timeout=1s' \
    --volume hololive-bot_holo-pg-admin-socket:/var/run/postgresql:ro \
    --entrypoint psql "$postgres_image" --no-psqlrc -v ON_ERROR_STOP=1 -At \
    -c "SELECT current_setting('transaction_read_only'), COALESCE((SELECT checksum_sha256::text FROM schema_migration_checksums WHERE filename = '$migration'), '')")" \
    || status="$?"
  leftover="$(podman ps --all --quiet --filter "label=$owner")" || return 1
  # shellcheck disable=SC2086 # container ID 목록을 인자로 나눈다.
  [[ -z "$leftover" ]] || podman rm --force $leftover >/dev/null
  [[ "$status" -eq 0 ]] || { echo 'collector migration proof query failed' >&2; return 1; }
  if [[ "$observed" != "on|$expected" ]]; then
    echo "collector requires the reviewed $migration; apply central db-migrate before paired cutover" >&2
    return 1
  fi
}

{
if [[ "$mode" == rollback ]]; then
  restore
  exit 0
fi
if [[ "$mode" == check ]]; then
  [[ -r "$backup/snapshot-complete" && -r "$current/$manifest" ]]
  po_verify_image "$issuer" "$current/$manifest" "$revision" arm64 "$current/$image_id_file"
  verify_collector "$collector" "$staging/collector-manifest.json" "$revision" "$staging/collector-image-id"
  [[ "$(image_label "$issuer" version)" == "$version" ]]
  [[ "$(image_label "$collector" version)" == "$version" ]]
  verify_issuer_runtime "$revision"
  verify_collector_runtime "$revision"
  since="$(cat "$backup/change-started-at")"
  [[ "$since" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]
  since_epoch="$(date -u -d "$since" +%s)"
  for container in "$issuer_container" "$collector_container"; do
    started_since "$since_epoch" "$container"
    require_post_cutover_logs "$since" "$container"
  done
  echo "$host_label collector/issuer completion verified revision=$revision backup=$backup"
  exit 0
fi
cutover_units_settled || {
  echo "$host_label issuer/collector manager jobs are unsettled or unavailable; no paired cutover started" >&2
  exit 1
}
systemctl is-active --quiet "$collector_unit" || {
  echo "$host_label collector must be running for a paired cutover; $collector_unit is not active" >&2
  exit 1
}
if [[ "$slot" == c ]]; then
  require_collector_lease_migration
fi
[[ -r "$staging/image.tar" && -r "$staging/image.tar.sha256" && -r "$staging/image-id" &&
   -r "$staging/collector-image.tar" && -r "$staging/collector-image.tar.sha256" && -r "$staging/collector-image-id" &&
   -r "$staging/rootfs-manifest.json" && -r "$staging/collector-manifest.json" ]] || exit 1
issuer_state="$(issuer_presence)"
if [[ "$issuer_state" == present ]] && [[ ! -r "$current/$manifest" || ! -r "$current/$image_id_file" ]]; then
  echo "$host_label issuer receipt missing: restore the reviewed $current/$manifest and $current/$image_id_file before cutover" >&2
  exit 1
fi
[[ ! -e "$backup" ]] || { echo "$host_label backup path already exists" >&2; exit 1; }
install -d -m 0700 -o root -g root "$backup"
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
python3 "$pair_tool" capture "$service" "$profile_release/previous" "$collector_container" "$current"
python3 "$pair_tool" rollback "$service" "$profile_release/previous"
python3 "$pair_tool" check "$service" "$profile_release/previous"
printf '%s\n' "$old_revision" > "$backup/collector.revision"
podman tag "$collector" "$collector_rollback"
if [[ "$issuer_state" == present ]]; then
  running_issuer_id="$(container_image_id "$issuer_container")"
  issuer_image_id="$(image_id "$issuer")"
  [[ "$running_issuer_id" == "$issuer_image_id" ]] || exit 1
  old_po_revision="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_revision"])' "$current/$manifest")"
  po_verify_image "$issuer" "$current/$manifest" "$old_po_revision" arm64 "$current/$image_id_file"
  [[ "$old_po_revision" == "$old_revision" ]]
  old_po_version="$(image_label "$issuer" version)"
  old_version="$(image_label "$collector" version)"
  [[ -n "$old_version" && "$old_po_version" == "$old_version" ]]
  cp "$current/$manifest" "$backup/po-prechange-manifest.json"
  cp "$current/$image_id_file" "$backup/po-prechange.image-id"
  podman tag "$issuer" "$issuer_rollback"
  printf 'present\n' > "$backup/issuer.state"
else
  [[ ! -e "$current/$manifest" && ! -e "$current/$image_id_file" ]]
  printf 'absent\n' > "$backup/issuer.state"
fi
printf '%s\n' "$revision" > "$backup/candidate.revision"
printf '%s\n' "$version" > "$backup/candidate.version"
: > "$backup/snapshot-complete"
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
cutover_restore_owner_pid="$BASHPID"
trap 'restore_after_failed_cutover $?' ERR
trap 'restore_after_failed_cutover 130' INT
trap 'restore_after_failed_cutover 143' TERM
trap 'restore_after_failed_cutover 129' HUP
(cd "$staging" && sha256sum --check --strict image.tar.sha256 collector-image.tar.sha256)
podman load --input "$staging/image.tar"
podman load --input "$staging/collector-image.tar"
po_verify_image "$issuer_candidate" "$staging/rootfs-manifest.json" "$revision" arm64 "$staging/image-id"
verify_collector "$collector_candidate" "$staging/collector-manifest.json" "$revision" "$staging/collector-image-id"
[[ "$(image_label "$issuer_candidate" version)" == "$version" ]]
[[ "$(image_label "$collector_candidate" version)" == "$version" ]]
candidate_image="$(image_id "$collector_candidate")"
python3 "$pair_tool" candidate "$service" "$profile_release/candidate" "$candidate_image" \
  < "$unit_dir/$collector_container.container"
python3 "$pair_tool" check "$service" "$profile_release/candidate"
python3 "$pair_tool" verify-source "$service" "$profile_release/candidate"
cutover_started=true
python3 "$pair_tool" activate "$service" "$profile_release/candidate" "$current"
podman tag "$issuer_candidate" "$issuer"
podman tag "$collector_candidate" "$collector"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf '%s\n' "$started_at" > "$backup/change-started-at"
systemctl daemon-reload
systemctl restart "$issuer_unit"
verify_issuer_runtime "$revision"
install -D -m 0644 "$staging/rootfs-manifest.json" "$current/$manifest"
install -D -m 0644 "$staging/image-id" "$current/$image_id_file"
systemctl restart "$collector_unit"
verify_collector_runtime "$revision"
python3 "$pair_tool" verify-live "$service" "$profile_release/candidate" "$collector_container"
since_epoch="$(date -u -d "$started_at" +%s)"
for container in "$issuer_container" "$collector_container"; do
  started_since "$since_epoch" "$container"
  require_post_cutover_logs "$started_at" "$container"
done
trap - ERR INT TERM HUP
printf '%s issuer-first cutover verified revision=%s backup=%s started_at=%s\n' "$host_label" "$revision" "$backup" "$started_at"
}
