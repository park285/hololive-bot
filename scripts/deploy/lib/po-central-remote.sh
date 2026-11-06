#!/usr/bin/env bash
# kapu에서 SSH로 호출하며 검증된 Quadlet collector·PO issuer pair의 교체만 수행한다.
# slot c는 중앙(hololive-seoul), slot b는 iris-seoul이며 둘 다 rootful Podman Quadlet이다. collector image는
# worker-profile-pair.py가 만드는 drop-in(50-worker-profile.conf, Image=sha256:<id>)이 고정하고, issuer는 unit 기본
# Image=의 localhost :prod tag를 쓴다. unit 정의 설치는 c는 sync-opt-current.sh, b는 deploy/podman/iris-seoul 절차가
# 소유하므로 이 스크립트는 unit 파일을 설치하지 않는다(롤백의 기본 unit 복원만 예외).
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
current="$remote_root/current"
unit_dir=/etc/containers/systemd/hololive-bot
manifest="../po-current-$slot/rootfs-manifest.json"
image_id_file="../po-current-$slot/image-id"
change_id="${backup##*/po-"$slot"-}"
# 활성 pair는 백업 정리 뒤에도 bind되므로 release 저장소에 둔다.
profile_release="$current/artifacts/worker-profile/releases/po-$slot-$change_id/$service"
collector=localhost/hololive-youtube-collector:prod
issuer=localhost/hololive-youtube-po-sandbox:prod
# kapu archive는 :candidate 이름으로 적재된다. Quadlet은 재시작할 때마다 Image= tag를 다시 읽으므로
# :prod는 cutover에서만 옮겨 issuer가 cutover 전에 스스로 재시작해도 이전 image로 뜨게 한다.
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

# Podman .Id·.Image는 접두어 없는 config digest다. 영수증과 drop-in의 identity는 sha256:<hex>다.
# 두 조회를 비교할 때는 변수에 먼저 받는다. [[ "$(a)" == "$(b)" ]]는 둘 다 실패하면 빈 값끼리 같아 통과한다.
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
# 조회 실패(종료 코드 125 등)를 없음으로 바꾸지 않는다.
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
# Notify=healthy라 systemctl start/restart는 healthcheck가 통과해야 성공한다. 그 뒤 실행 중인 container가
# 기대한 image·revision·socket 권한인지 다시 확인한다.
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
    # grep의 불일치(1)만 정상입니다. 조회·검사 실패를 완료 성공으로 바꾸지 않습니다.
    [[ "$status" -eq 1 ]] || return "$status"
  fi
}
restore_collector_base_unit() {
  local recorded="$profile_release/previous/unit.container" installed="$unit_dir/$collector_container.container" tmp
  [[ -f "$recorded" ]] || { echo "[WARN] $recorded is missing; the installed collector unit definition stays as is" >&2; return 0; }
  ! cmp -s -- "$recorded" "$installed" || return 0
  cp -p -- "$installed" "$profile_release/rollback-replaced-$collector_container.container" || return
  tmp="$(mktemp "$unit_dir/.$collector_container.container.XXXXXX")" || return
  install -m 0644 -- "$recorded" "$tmp" && mv -f -- "$tmp" "$installed" || { rm -f -- "$tmp"; return 1; }
  echo "[ROLLBACK] restored the collector base unit the previous image ran with; reinstall the intended revision's unit before the next deployment" >&2
}
restore() {
  local old_revision old_po_revision state issuer_now
  trap - ERR INT TERM HUP
  [[ -r "$backup/snapshot-complete" ]] || { echo "$host_label rollback snapshot incomplete" >&2; return 1; }
  state="$(cat "$backup/issuer.state")"
  old_revision="$(cat "$backup/collector.revision")"
  verify_collector "$collector_rollback" "$backup/collector-prechange-manifest.json" "$old_revision" "$backup/collector-prechange.image-id"
  if [[ "$state" == present ]]; then
    old_po_revision="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_revision"])' "$backup/po-prechange-manifest.json")"
    po_verify_image podman "$issuer_rollback" "$backup/po-prechange-manifest.json" "$old_po_revision" arm64 "$backup/po-prechange.image-id"
  else
    [[ "$state" == absent ]] || return 1
  fi
  python3 "$pair_tool" verify "$service" "$profile_release/previous" "$collector_rollback"
  python3 "$pair_tool" check "$service" "$profile_release/previous"
  # 복원 drop-in 게시가 실패하면 실행 중인 issuer·collector를 건드리지 않는다.
  python3 "$pair_tool" activate "$service" "$profile_release/previous" "$current" || return
  # pair는 이전 image가 돌던 기본 unit(unit.container, verify가 checksum 확인)도 보존한다. sync-opt-current.sh가 새
  # revision의 unit을 이미 설치했으면 이전 image를 새 정의(env·mount·healthcheck)로 띄우지 않게 함께 되돌린다
  # (quadlet-rollback-service.sh와 같은 규칙). 바뀐 unit은 release에 남긴다.
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
  local migration=222_drop_youtube_job_lease_legacy_failure_trigger.sql expected observed collector_env
  local migrator_user postgres_image owner leftover status=0
  expected="$(sha256sum "$staging/hololive/hololive-api/scripts/migrations/$migration")"
  expected="${expected%% *}"
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || return 1
  # 격리 소켓으로 원장을 보기 전에 수집기가 이 주 서버를 가리키는지 확인한다.
  collector_env="$(podman inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$collector_container")" || return 1
  if ! grep -qx 'POSTGRES_HOST=holo-postgres' <<<"$collector_env" || ! grep -qx 'POSTGRES_PORT=5432' <<<"$collector_env" \
      || ! grep -qx 'POSTGRES_DB=hololive' <<<"$collector_env"; then
    echo 'collector migration proof requires the local primary route' >&2
    return 1
  fi
  # hololive-db-migrate.container와 같은 역할·소켓을 쓴다. compose.env의 HOLOLIVE_MIGRATOR_USER가 unit 기본값을
  # 덮으며, prestart가 거부하지 않는 전체 따옴표만 벗긴다.
  migrator_user="$(awk -F= '$1 == "HOLOLIVE_MIGRATOR_USER" { v = substr($0, length($1) + 2); gsub(/^["\047]|["\047]$/, "", v); print v }' \
    /etc/stack-secrets/hololive-bot/compose.env)"
  migrator_user="${migrator_user:-hololive_migrator}"
  [[ "$migrator_user" =~ ^[a-z_][a-z0-9_]*$ ]] || { echo 'collector migration proof requires a valid migrator role' >&2; return 1; }
  postgres_image="$(podman inspect -f '{{.Image}}' holo-postgres)" || return 1
  [[ "$postgres_image" =~ ^[0-9a-f]{64}$ ]] || { echo 'collector migration proof client identity is invalid' >&2; return 1; }
  owner="hololive.collector-migration-proof=$(cat /proc/sys/kernel/random/uuid)"
  # 관리 socket은 pg_hba의 local trust 경로다. 비밀번호 없이 읽기 전용 transaction으로만 원장을 읽는다.
  observed="$(timeout 20 podman run --rm --label "$owner" --pull=never --read-only --cap-drop=all \
    --security-opt=no-new-privileges --network none \
    --env PGHOST=/var/run/postgresql --env PGPORT=5432 --env PGDATABASE=hololive --env "PGUSER=$migrator_user" \
    --env PGSSLMODE=disable --env PGCONNECT_TIMEOUT=5 --env PGAPPNAME=hololive-collector-migration-proof \
    --env 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=5s -c lock_timeout=1s' \
    --volume hololive-bot_holo-pg-admin-socket:/var/run/postgresql:ro \
    --entrypoint psql "$postgres_image" --no-psqlrc -v ON_ERROR_STOP=1 -At \
    -c "SELECT current_setting('transaction_read_only'), COALESCE((SELECT checksum_sha256::text FROM schema_migration_checksums WHERE filename = '$migration'), '')")" \
    || status="$?"
  # timeout이 podman client만 끝낸 경우에도 이 검사가 만든 container는 남기지 않는다.
  leftover="$(podman ps --all --quiet --filter "label=$owner")" || return 1
  # shellcheck disable=SC2086 # container ID 목록을 인자로 나눈다.
  [[ -z "$leftover" ]] || podman rm --force $leftover >/dev/null
  [[ "$status" -eq 0 ]] || { echo 'collector migration proof query failed' >&2; return 1; }
  if [[ "$observed" != "on|$expected" ]]; then
    echo "collector requires the reviewed $migration; apply central db-migrate before paired cutover" >&2
    return 1
  fi
}

# 이 본문은 ssh bash -s의 stdin으로 도착한다. 실행 구간을 한 묶음으로 끝까지 읽은 뒤 실행해야
# stdin을 읽는 자식 명령이 남은 본문을 소비해 교체 없이 exit 0으로 끝나지 않는다.
{
if [[ "$mode" == rollback ]]; then
  restore
  exit 0
fi
if [[ "$mode" == check ]]; then
  [[ -r "$backup/snapshot-complete" && -r "$current/$manifest" ]]
  po_verify_image podman "$issuer" "$current/$manifest" "$revision" arm64 "$current/$image_id_file"
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
# Quadlet은 정지한 unit의 container를 지운다(--rm). 이전 pair의 profile은 실행 중인 collector에서만 증명할 수 있어
# collector를 멈춘 채 교체하는 점검 모드는 지원하지 않는다.
systemctl is-active --quiet "$collector_unit" || {
  echo "$host_label collector must be running for a paired cutover; $collector_unit is not active" >&2
  exit 1
}
# W4 collector는 옛 lease failure 진단 복원 코드를 제거했으므로 222 적용 증명 전에는 교체하지 않는다.
# 원장은 중앙 주 서버의 관리 socket으로만 읽을 수 있어 c에서 증명한다. b는 같은 release의 c 배포 뒤에 배포한다.
if [[ "$slot" == c ]]; then
  require_collector_lease_migration
fi
[[ -r "$staging/image.tar" && -r "$staging/image.tar.sha256" && -r "$staging/image-id" &&
   -r "$staging/collector-image.tar" && -r "$staging/collector-image.tar.sha256" && -r "$staging/collector-image-id" &&
   -r "$staging/rootfs-manifest.json" && -r "$staging/collector-manifest.json" ]] || exit 1
issuer_state="$(issuer_presence)"
# issuer 기준 영수증(po-current-<slot>)은 current 밖의 호스트 상태다. issuer 이미지가 있는데 영수증이 없으면 이전 pair를
# 복원할 근거가 없으므로, 백업·rollback 태그를 만들기 전에 원인을 남기고 거절한다(호스트 교체 때 누락된 사례).
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
  po_verify_image podman "$issuer" "$current/$manifest" "$old_po_revision" arm64 "$current/$image_id_file"
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
# 이전 pair snapshot을 확보한 뒤에만 복원을 무장합니다.
restore_after_failed_cutover() {
  local status="${1:-1}" restore_status
  # 상속된 ERR trap의 자식은 실패를 부모에 넘기고, cutover 소유 shell에서만 한 번 복원합니다.
  if [[ "$BASHPID" != "${cutover_restore_owner_pid:?cutover restore owner not armed}" ]]; then
    exit "$status"
  fi
  trap - ERR INT TERM HUP
  set +e
  if [[ "$cutover_started" == true ]]; then
    # restore를 ||/if 조건에서 호출하면 함수 내부 errexit가 꺼집니다.
    (set -e; restore)
    restore_status="$?"
  else
    # 후보 검사 실패는 :prod tag·drop-in·실행 중 pair를 바꾸기 전이다. :candidate 적재물과 백업만 남긴다.
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
po_verify_image podman "$issuer_candidate" "$staging/rootfs-manifest.json" "$revision" arm64 "$staging/image-id"
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
