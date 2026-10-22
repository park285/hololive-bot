#!/usr/bin/env bash
# kapu에서 SSH로 호출하며 검증된 서비스의 교체만 수행한다.
set -Eeuo pipefail
[[ $# -eq 5 ]] || exit 2
mode="$1" staging="$2" backup="$3" revision="$4" version="$5"
[[ "$mode" == deploy || "$mode" == rollback || "$mode" == check ]] || exit 2
[[ "$staging" =~ ^/opt/hololive-bot/compose/po-staging-[0-9]{8}T[0-9]{6}Z$ ]] || exit 2
[[ "$backup" =~ ^/opt/hololive-bot/compose/backups/po-c-[0-9]{8}T[0-9]{6}Z$ ]] || exit 2
[[ "$revision" =~ ^[0-9a-f]{40}$ && "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 2
current=/opt/hololive-bot/compose/current
manifest=../po-current-c/rootfs-manifest.json
image_id_file=../po-current-c/image-id
# 중앙 collector-c는 prod+live-compat의 youtube-collector다.
files=(hololive/hololive-api/VERSION hololive/hololive-alarm-worker/VERSION
       deploy/compose/docker-compose.prod.yml deploy/compose/docker-compose.live-compat.yml
       deploy/compose/docker-compose.collector.yml
       scripts/build/po-sandbox-manifest.py scripts/deploy/lib/po-sandbox-image.sh
       scripts/deploy/lib/worker-profile-pair.py scripts/deploy/compose.sh)
collector=hololive-youtube-collector:prod
issuer=hololive-youtube-po-sandbox:prod
collector_container=hololive-youtube-collector-c
issuer_container=hololive-youtube-po-c
[[ "$(stat -c '%u:%g %a' "$staging")" == '0:0 700' ]] || { echo 'central staged artifacts are not root-private' >&2; exit 1; }
cd "$current"
. "$staging/scripts/deploy/lib/po-sandbox-image.sh"
pair_tool="$staging/scripts/deploy/lib/worker-profile-pair.py"
cutover_started=false

compose() {
  local profile_args=()
  if [[ -n "${WORKER_PROFILE_OVERRIDE_FILE:-}" && "$WORKER_PROFILE_OVERRIDE_FILE" != none ]]; then
    profile_args=(-f "$WORKER_PROFILE_OVERRIDE_FILE")
  fi
  COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env \
  HOLO_API_VERSION="$(cat hololive/hololive-api/VERSION)" \
  REVISION="$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$collector")" \
    ./scripts/deploy/compose.sh \
    -f deploy/compose/docker-compose.prod.yml \
    -f deploy/compose/docker-compose.live-compat.yml "${profile_args[@]}" "$@"
}
verify_collector() {
  local image="$1" reviewed="$2" expected="$3" id_file="$4" expected_id actual_id actual_arch actual_revision
  [[ "$expected" =~ ^[0-9a-f]{40}$ ]] || return 1
  expected_id="$(cat "$id_file")"
  actual_id="$(docker image inspect -f '{{.Id}}' "$image")"
  po_image_id_matches "$actual_id" "$expected_id" || return 1
  python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); assert m["source_revision"] == sys.argv[2] and m["go"]["goarch"] == "arm64" and m.get("image_id",sys.argv[3]) == sys.argv[3]' "$reviewed" "$expected" "$actual_id"
  actual_arch="$(docker image inspect -f '{{.Os}}/{{.Architecture}}' "$image")"
  actual_revision="$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")"
  [[ "$actual_arch" == linux/arm64 && "$actual_revision" == "$expected" ]]
}
wait_issuer() {
  local started="$1" status socket_mount actual
  for _ in $(seq 1 30); do
    status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$issuer_container")"
    [[ "$status" == healthy ]] && break
    sleep 2
  done
  [[ "$status" == healthy ]]
  actual="$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$issuer_container")"
  [[ "$actual" == "$started" ]]
  [[ "$(docker inspect -f '{{.Image}}' "$issuer_container")" == "$(docker image inspect -f '{{.Id}}' "$issuer")" ]]
  socket_mount="$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/run/hololive-youtube-po"}}{{.Source}}{{end}}{{end}}' "$issuer_container")"
  [[ "$(stat -c '%u:%g %a' "$socket_mount")" == '65532:1000 770' ]]
  docker exec "$issuer_container" /app/bin/po-broker --healthcheck --socket /run/hololive-youtube-po/worker.sock
}
wait_collector() {
  local expected="$1" status actual
  for _ in $(seq 1 90); do
    status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$collector_container")"
    [[ "$status" == healthy ]] && break
    sleep 2
  done
  [[ "$status" == healthy ]]
  actual="$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$collector_container")"
  [[ "$actual" == "$expected" ]]
  [[ "$(docker inspect -f '{{.Image}}' "$collector_container")" == "$(docker image inspect -f '{{.Id}}' "$collector")" ]]
  docker exec "$collector_container" ./bin/healthcheck https://127.0.0.1:30025/ready >/dev/null
}
require_post_cutover_logs() {
  local since="$1" container="$2" output status
  output="$(docker logs --since "$since" "$container" 2>&1)" || {
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
restore() {
  local old_revision old_po_revision state collector_state rel
  trap - ERR INT TERM HUP
  [[ -r "$backup/snapshot-complete" ]] || { echo 'central rollback snapshot incomplete' >&2; return 1; }
  state="$(cat "$backup/issuer.state")"
  collector_state="$(cat "$backup/collector-prechange.state")"
  [[ "$collector_state" == running || "$collector_state" == stopped ]] || return 1
  old_revision="$(cat "$backup/collector.revision")"
  verify_collector hololive-youtube-collector:rollback-"${backup##*/po-c-}" "$backup/collector-prechange-manifest.json" "$old_revision" "$backup/collector-prechange.image-id"
  if [[ "$state" == present ]]; then
    old_po_revision="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_revision"])' "$backup/po-prechange-manifest.json")"
    po_verify_image "hololive-youtube-po-sandbox:rollback-${backup##*/po-c-}" "$backup/po-prechange-manifest.json" "$old_po_revision" arm64 "$backup/po-prechange.image-id"
  else
    [[ "$state" == absent ]] || return 1
  fi
  python3 "$pair_tool" verify youtube-collector "$backup/worker-profile-previous" "hololive-youtube-collector:rollback-${backup##*/po-c-}"
  HOLO_API_VERSION="$(cat "$backup/previous-files/hololive/hololive-api/VERSION")" \
  HOLO_ALARM_WORKER_VERSION="$(cat "$backup/previous-files/hololive/hololive-alarm-worker/VERSION")" \
    docker compose --env-file /etc/stack-secrets/hololive-bot/compose.env \
    --project-directory "$current/deploy/compose" \
    -f "$backup/previous-files/deploy/compose/docker-compose.prod.yml" \
    -f "$backup/previous-files/deploy/compose/docker-compose.live-compat.yml" \
    -f "$backup/worker-profile-previous/compose.yml" \
    run --rm --no-deps --pull never youtube-collector --check-worker-profile
  # 복원 profile 게시가 실패해도 현재 issuer를 먼저 제거하지 않는다.
  python3 "$pair_tool" activate youtube-collector "$backup/worker-profile-previous" "$current" || return
  docker stop "$issuer_container" >/dev/null 2>&1 || true
  docker rm -f "$issuer_container" >/dev/null 2>&1 || true
  docker tag "hololive-youtube-collector:rollback-${backup##*/po-c-}" "$collector"
  for rel in "${files[@]}"; do
    [[ "$rel" != scripts/deploy/compose.sh ]] || continue
    if [[ -f "$backup/previous-files/$rel" ]]; then
      install -D -m 0644 "$backup/previous-files/$rel" "$current/$rel"
    else
      rm -f "$current/$rel"
    fi
  done
  install -m 0755 "$staging/scripts/deploy/compose.sh" "$current/scripts/deploy/compose.sh"
  export WORKER_PROFILE_OVERRIDE_FILE="$backup/worker-profile-previous/compose.yml"
  if [[ "$state" == present ]]; then
    docker tag "hololive-youtube-po-sandbox:rollback-${backup##*/po-c-}" "$issuer"
    install -D -m 0644 "$backup/po-prechange-manifest.json" "$current/$manifest"
    install -D -m 0644 "$backup/po-prechange.image-id" "$current/$image_id_file"
    compose config --quiet
    compose up -d --no-build --no-deps --force-recreate "$issuer_container_service"
    wait_issuer "$old_po_revision"
  else
    rm -f "$current/$manifest" "$current/$image_id_file"
    if docker image inspect "$issuer" >/dev/null 2>&1; then docker image rm "$issuer" >/dev/null; fi
    compose config --quiet
  fi
  if [[ "$collector_state" == running ]]; then
    compose up -d --no-build --no-deps --force-recreate youtube-collector || return
    wait_collector "$old_revision" || return
  else
    compose up --no-start --no-build --no-deps --force-recreate youtube-collector || return
    [[ "$(docker inspect -f '{{.State.Running}}' "$collector_container")" == false ]] || return 1
    [[ "$(docker inspect -f '{{.Image}}' "$collector_container")" == "$(cat "$backup/collector-prechange.image-id")" ]] || return 1
  fi
  echo "central previous collector/issuer restored from $backup collector_state=$collector_state"
}
require_collector_lease_migration() {
  local migration=222_drop_youtube_job_lease_legacy_failure_trigger.sql expected observed
  expected="$(sha256sum "$staging/hololive/hololive-api/scripts/migrations/$migration")"
  expected="${expected%% *}"
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || return 1
  # 격리 소켓으로 원장을 보기 전에 수집기가 이 주 서버를 가리키는지 확인한다.
  observed="$(compose config --format json | python3 -c '
import json, os, subprocess, sys, uuid
config = json.load(sys.stdin)
services = config["services"]
migrator = services["hololive-db-migrate"]
source = migrator["environment"]
collector = services["youtube-collector"]["environment"]
if source.get("PGHOST") != "/var/run/postgresql" or source.get("PGSSLMODE") != "disable":
    sys.exit("migration proof requires the isolated central socket")
if collector.get("POSTGRES_HOST") != "holo-postgres" or str(collector.get("POSTGRES_PORT")) != "5432" or source["PGDATABASE"] != collector["POSTGRES_DB"]:
    sys.exit("collector migration proof requires the local primary route")
names = ("PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSSLMODE")
if any(not source.get(name) for name in names):
    sys.exit("collector migration proof requires complete migrator connection settings")
mounts = [v for v in migrator["volumes"] if v.get("type") == "volume" and v.get("target") == source["PGHOST"]]
if len(mounts) != 1 or not mounts[0].get("read_only"):
    sys.exit("collector migration proof requires the read-only admin socket mount")
socket_volume = config["volumes"][mounts[0]["source"]]["name"]
env = os.environ.copy()
env.update({name: str(source[name]) for name in names})
env.update(PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s -c lock_timeout=1s", PGCONNECT_TIMEOUT="5", PGAPPNAME="hololive-collector-migration-proof")
owner = "hololive.collector-migration-proof=" + uuid.uuid4().hex
command = ["docker", "create", "--label", owner, "--pull=never", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--network", "none", "--env", "PGAPPNAME"]
for name in (*names, "PGOPTIONS", "PGCONNECT_TIMEOUT"):
    command.extend(("--env", name))
command.extend(("--mount", "type=volume,src=" + socket_volume + ",dst=" + source["PGHOST"] + ",readonly", "--entrypoint", "psql", services["holo-postgres"]["image"], "--no-psqlrc", "-v", "ON_ERROR_STOP=1", "-At", "-c", sys.argv[1]))
try:
    created = subprocess.run(command, env=env, text=True, capture_output=True, timeout=15)
    if created.returncode:
        sys.exit("collector migration proof client creation failed")
    container = created.stdout.strip()
    if len(container) != 64 or any(c not in "0123456789abcdef" for c in container):
        sys.exit("collector migration proof client identity is invalid")
    result = subprocess.run(["docker", "start", "--attach", container], text=True, capture_output=True, timeout=15)
    if result.returncode:
        sys.exit("collector migration proof query failed")
    print(result.stdout, end="")
finally:
    owned = subprocess.run(["docker", "ps", "--all", "--quiet", "--no-trunc", "--filter", "label=" + owner], check=True, text=True, capture_output=True, timeout=10).stdout.split()
    for container in owned:
        subprocess.run(["docker", "rm", "--force", "--volumes", container], check=True, stdout=subprocess.DEVNULL, timeout=10)
' "SELECT current_setting('transaction_read_only'), COALESCE((SELECT checksum_sha256::text FROM schema_migration_checksums WHERE filename = '$migration'), '')")" || return 1
  if [[ "$observed" != "on|$expected" ]]; then
    echo "collector requires the reviewed $migration; apply central db-migrate before paired cutover" >&2
    return 1
  fi
}

issuer_container_service=youtube-po-c
# 이 본문은 ssh bash -s의 stdin으로 도착한다. 실행 구간을 한 묶음으로 끝까지 읽은 뒤 실행해야
# stdin을 붙이는 compose run이 남은 본문을 읽어 교체 없이 exit 0으로 끝나지 않는다.
{
if [[ "$mode" == rollback ]]; then
  restore
  exit 0
fi
if [[ "$mode" == check ]]; then
  [[ -r "$backup/snapshot-complete" && -r "$current/$manifest" ]]
  po_verify_image "$issuer" "$current/$manifest" "$revision" arm64 "$current/$image_id_file"
  verify_collector "$collector" "$staging/collector-manifest.json" "$revision" "$staging/collector-image-id"
  [[ "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$issuer")" == "$version" ]]
  [[ "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$collector")" == "$version" ]]
  compose config --quiet
  wait_issuer "$revision"
  wait_collector "$revision"
  since="$(cat "$backup/change-started-at")"
  [[ "$since" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]
  since_epoch="$(date -u -d "$since" +%s)"
  for container in "$issuer_container" "$collector_container"; do
    [[ "$(date -u -d "$(docker inspect -f '{{.State.StartedAt}}' "$container")" +%s)" -ge "$since_epoch" ]]
    require_post_cutover_logs "$since" "$container"
  done
  echo "central collector/issuer completion verified revision=$revision backup=$backup"
  exit 0
fi
# W4 collector는 옛 lease failure 진단 복원 코드를 제거했으므로 222 적용 증명 전에는 교체하지 않는다.
require_collector_lease_migration
[[ -r "$staging/image.tar" && -r "$staging/image.tar.sha256" && -r "$staging/image-id" &&
   -r "$staging/collector-image.tar" && -r "$staging/collector-image.tar.sha256" && -r "$staging/collector-image-id" &&
   -r "$staging/rootfs-manifest.json" && -r "$staging/collector-manifest.json" ]] || exit 1
# issuer 기준 영수증(po-current-c)은 current 밖의 호스트 상태다. issuer 이미지가 있는데 영수증이 없으면 이전 pair를
# 복원할 근거가 없으므로, 백업·rollback 태그를 만들기 전에 원인을 남기고 거절한다(호스트 교체 때 누락된 사례).
if docker image inspect "$issuer" >/dev/null 2>&1 && [[ ! -r "$current/$manifest" || ! -r "$current/$image_id_file" ]]; then
  echo "central issuer receipt missing: restore the reviewed $current/$manifest and $current/$image_id_file before cutover" >&2
  exit 1
fi
[[ ! -e "$backup" ]] || { echo 'central backup path already exists' >&2; exit 1; }
install -d -m 0700 -o root -g root "$backup/previous-files"
[[ "$(docker inspect -f '{{.Image}}' "$collector_container")" == "$(docker image inspect -f '{{.Id}}' "$collector")" ]] || exit 1
old_revision="$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$collector")"
[[ "$old_revision" =~ ^[0-9a-f]{40}$ ]] || exit 1
old_image_id="$(docker image inspect -f '{{.Id}}' "$collector")"
printf '%s\n' "$old_image_id" > "$backup/collector-prechange.image-id"
python3 - "$old_revision" "$old_image_id" "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$collector")" "$backup/collector-prechange-manifest.json" <<'PY'
import json, sys
json.dump({'schema_version': 1, 'source_revision': sys.argv[1], 'go': {'goarch': 'arm64'}, 'image_id': sys.argv[2], 'version': sys.argv[3]}, open(sys.argv[4], 'w'))
PY
verify_collector "$collector" "$backup/collector-prechange-manifest.json" "$old_revision" "$backup/collector-prechange.image-id"
case "$(docker inspect -f '{{.State.Running}}' "$collector_container")" in
  true) printf 'running\n' > "$backup/collector-prechange.state" ;;
  false) printf 'stopped\n' > "$backup/collector-prechange.state" ;;
  *) echo 'central collector running state unavailable' >&2; exit 1 ;;
esac
python3 "$pair_tool" capture youtube-collector "$backup/worker-profile-previous" "$collector_container" "$current"
WORKER_PROFILE_OVERRIDE_FILE=none compose config --format json \
  | python3 "$pair_tool" rollback youtube-collector "$backup/worker-profile-previous"
WORKER_PROFILE_OVERRIDE_FILE="$backup/worker-profile-previous/compose.yml" compose run --rm --no-deps --pull never youtube-collector --check-worker-profile
printf '%s\n' "$old_revision" > "$backup/collector.revision"
docker tag "$collector" "hololive-youtube-collector:rollback-${backup##*/po-c-}"
if docker image inspect "$issuer" >/dev/null 2>&1; then
  [[ -r "$current/$manifest" ]]
  [[ "$(docker inspect -f '{{.Image}}' "$issuer_container")" == "$(docker image inspect -f '{{.Id}}' "$issuer")" ]] || exit 1
  old_po_revision="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_revision"])' "$current/$manifest")"
  po_verify_image "$issuer" "$current/$manifest" "$old_po_revision" arm64 "$current/$image_id_file"
  [[ "$old_po_revision" == "$old_revision" ]]
  [[ "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$issuer")" == "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$collector")" ]]
  cp "$current/$manifest" "$backup/po-prechange-manifest.json"
  cp "$current/$image_id_file" "$backup/po-prechange.image-id"
  docker tag "$issuer" "hololive-youtube-po-sandbox:rollback-${backup##*/po-c-}"
  printf 'present\n' > "$backup/issuer.state"
else
  [[ ! -e "$current/$manifest" && ! -e "$current/$image_id_file" ]]
  printf 'absent\n' > "$backup/issuer.state"
fi
for rel in "${files[@]}"; do
  [[ ! -f "$current/$rel" ]] || { mkdir -p "$backup/previous-files/$(dirname "$rel")"; cp -a "$current/$rel" "$backup/previous-files/$rel"; }
done
printf '%s\n' "$revision" > "$backup/candidate.revision"
printf '%s\n' "$version" > "$backup/candidate.version"
: > "$backup/snapshot-complete"
# 이전 pair와 설정 snapshot을 확보한 뒤에만 복원을 무장합니다.
restore_after_failed_cutover() {
  local status="${1:-1}" restore_status
  # 상속된 ERR trap의 자식은 실패를 부모에 넘기고, cutover 소유 shell에서만 한 번 복원합니다.
  if [[ "$BASHPID" != "${cutover_restore_owner_pid:?cutover restore owner not armed}" ]]; then
    exit "$status"
  fi
  trap - ERR INT TERM HUP
  set +e
  # restore를 ||/if 조건에서 호출하면 함수 내부 errexit가 꺼집니다.
  if [[ "$cutover_started" == true ]]; then
    (set -e; restore)
  else
    # 후보 검사 실패는 실행 중 pair를 재생성하지 않는다. 바뀐 태그와 배포 파일만 되돌린다.
    (set -e
      issuer_state="$(cat "$backup/issuer.state")"
      case "$issuer_state" in
        present|absent) ;;
        *) echo 'central issuer baseline state invalid' >&2; exit 1 ;;
      esac
      docker tag "hololive-youtube-collector:rollback-${backup##*/po-c-}" "$collector"
      if [[ "$issuer_state" == present ]]; then
        docker tag "hololive-youtube-po-sandbox:rollback-${backup##*/po-c-}" "$issuer"
      else
        issuer_images="$(docker image ls --quiet --filter "reference=$issuer")"
        if [[ -n "$issuer_images" ]]; then
          docker image rm "$issuer" >/dev/null
        fi
      fi
      for rel in "${files[@]}"; do
        if [[ -f "$backup/previous-files/$rel" ]]; then
          cp -a "$backup/previous-files/$rel" "$current/$rel"
        else
          rm -f "$current/$rel"
        fi
      done
    )
  fi
  restore_status="$?"
  if [[ "$restore_status" -ne 0 ]]; then
    echo "central automatic restoration failed; retain $backup" >&2
  fi
  exit "$status"
}
cutover_restore_owner_pid="$BASHPID"
trap 'restore_after_failed_cutover $?' ERR
trap 'restore_after_failed_cutover 130' INT
trap 'restore_after_failed_cutover 143' TERM
trap 'restore_after_failed_cutover 129' HUP
for rel in "${files[@]}"; do install -D -m 0644 "$staging/$rel" "$current/$rel"; done
chmod 0755 "$current/scripts/deploy/compose.sh"
(cd "$staging" && sha256sum --check --strict image.tar.sha256 collector-image.tar.sha256)
docker load --input "$staging/image.tar"
docker load --input "$staging/collector-image.tar"
po_verify_image "$issuer" "$staging/rootfs-manifest.json" "$revision" arm64 "$staging/image-id"
verify_collector "$collector" "$staging/collector-manifest.json" "$revision" "$staging/collector-image-id"
[[ "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$issuer")" == "$version" ]]
[[ "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$collector")" == "$version" ]]
compose config --quiet
WORKER_PROFILE_OVERRIDE_FILE=none compose config --format json \
  | python3 "$pair_tool" candidate youtube-collector "$backup/worker-profile-candidate"
WORKER_PROFILE_OVERRIDE_FILE="$backup/worker-profile-candidate/compose.yml" compose run --rm --no-deps --pull never youtube-collector --check-worker-profile
python3 "$pair_tool" verify-source youtube-collector "$backup/worker-profile-candidate"
cutover_started=true
python3 "$pair_tool" activate youtube-collector "$backup/worker-profile-candidate" "$current"
export WORKER_PROFILE_OVERRIDE_FILE="$backup/worker-profile-candidate/compose.yml"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf '%s\n' "$started_at" > "$backup/change-started-at"
compose up -d --no-build --no-deps --force-recreate "$issuer_container_service"
wait_issuer "$revision"
install -D -m 0644 "$staging/rootfs-manifest.json" "$current/$manifest"
install -D -m 0644 "$staging/image-id" "$current/$image_id_file"
compose up -d --no-build --no-deps --force-recreate youtube-collector
wait_collector "$revision"
python3 "$pair_tool" verify-live youtube-collector "$backup/worker-profile-candidate" "$collector_container"
since_epoch="$(date -u -d "$started_at" +%s)"
for container in "$issuer_container" "$collector_container"; do
  [[ "$(date -u -d "$(docker inspect -f '{{.State.StartedAt}}' "$container")" +%s)" -ge "$since_epoch" ]]
  require_post_cutover_logs "$started_at" "$container"
done
trap - ERR INT TERM HUP
printf 'central issuer-first cutover verified revision=%s backup=%s started_at=%s\n' "$revision" "$backup" "$started_at"
}
