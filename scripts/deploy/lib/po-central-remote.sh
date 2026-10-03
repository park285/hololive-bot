#!/usr/bin/env bash
# Invoked over SSH from the kapu build host as root. Only no-build service-only cutovers.
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
# 중앙 collector-c는 prod.yml의 youtube-collector다. 서비스를 재선언만 하던 빈 main-ap overlay 2종과 profile은
# 지웠으므로(stack-audit 2026-09-26 T11 holo-main-ap-empty-overlays) prod+live-compat 조합만 쓴다.
files=(hololive/hololive-api/VERSION hololive/hololive-alarm-worker/VERSION
       deploy/compose/docker-compose.prod.yml deploy/compose/docker-compose.live-compat.yml
       scripts/build/po-sandbox-manifest.py scripts/deploy/lib/po-sandbox-image.sh)
collector=hololive-youtube-collector:prod
issuer=hololive-youtube-po-sandbox:prod
collector_container=hololive-youtube-collector-c
issuer_container=hololive-youtube-po-c
[[ "$(stat -c '%u:%g %a' "$staging")" == '0:0 700' ]] || { echo 'central staged artifacts are not root-private' >&2; exit 1; }
cd "$current"
. "$staging/scripts/deploy/lib/po-sandbox-image.sh"

compose() {
  COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env \
  HOLO_API_VERSION="$(cat hololive/hololive-api/VERSION)" \
  REVISION="$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$collector")" \
    ./scripts/deploy/compose.sh \
    -f deploy/compose/docker-compose.prod.yml \
    -f deploy/compose/docker-compose.live-compat.yml "$@"
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
  local old_revision old_po_revision state rel
  trap - ERR INT TERM HUP
  [[ -r "$backup/snapshot-complete" ]] || { echo 'central rollback snapshot incomplete' >&2; return 1; }
  state="$(cat "$backup/issuer.state")"
  old_revision="$(cat "$backup/collector.revision")"
  verify_collector hololive-youtube-collector:rollback-"${backup##*/po-c-}" "$backup/collector-prechange-manifest.json" "$old_revision" "$backup/collector-prechange.image-id"
  if [[ "$state" == present ]]; then
    old_po_revision="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_revision"])' "$backup/po-prechange-manifest.json")"
    po_verify_image "hololive-youtube-po-sandbox:rollback-${backup##*/po-c-}" "$backup/po-prechange-manifest.json" "$old_po_revision" arm64 "$backup/po-prechange.image-id"
  else
    [[ "$state" == absent ]] || return 1
  fi
  docker stop "$issuer_container" >/dev/null 2>&1 || true
  docker rm -f "$issuer_container" >/dev/null 2>&1 || true
  docker tag "hololive-youtube-collector:rollback-${backup##*/po-c-}" "$collector"
  for rel in "${files[@]}"; do
    if [[ -f "$backup/previous-files/$rel" ]]; then
      install -D -m 0644 "$backup/previous-files/$rel" "$current/$rel"
    else
      rm -f "$current/$rel"
    fi
  done
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
  compose up -d --no-build --no-deps --force-recreate youtube-collector
  wait_collector "$old_revision"
  echo "central previous collector/issuer restored from $backup"
}
require_collector_lease_migration() {
  local migration=222_drop_youtube_job_lease_legacy_failure_trigger.sql expected observed
  expected="$(sha256sum "$staging/hololive/hololive-api/scripts/migrations/$migration")"
  expected="${expected%% *}"
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || return 1
  # Compose가 해석한 migrator 접속을 그대로 사용한다. 로컬 socket ledger는 외부 DB override의 증거가 아니다.
  observed="$(compose config --format json | python3 -c '
import json, os, subprocess, sys, uuid
config = json.load(sys.stdin)
services = config["services"]
migrator = services["hololive-db-migrate"]
source = migrator["environment"]
collector = services["youtube-collector"]["environment"]
for pg, runtime in (("PGHOST", "POSTGRES_HOST"), ("PGPORT", "POSTGRES_PORT"), ("PGDATABASE", "POSTGRES_DB")):
    if str(source[pg]) != str(collector[runtime]):
        sys.exit("collector and migrator database routes differ")
names = ("PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSSLMODE", "PGSSLROOTCERT")
if any(not source.get(name) for name in names):
    sys.exit("collector migration proof requires complete migrator connection settings")
networks = list(migrator["networks"])
if len(networks) != 1:
    sys.exit("collector migration proof requires one migrator network")
network = config["networks"][networks[0]]["name"]
roots = [v for v in migrator["volumes"] if v.get("type") == "bind" and v.get("target") == source["PGSSLROOTCERT"]]
if len(roots) != 1 or not roots[0].get("read_only"):
    sys.exit("collector migration proof requires the migrator read-only CA mount")
env = os.environ.copy()
env.update({name: str(source[name]) for name in names})
env.update(PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s -c lock_timeout=1s", PGCONNECT_TIMEOUT="5")
owner = "hololive.collector-migration-proof=" + uuid.uuid4().hex
command = ["docker", "create", "--label", owner, "--pull=never", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--network", network]
for resolver in migrator.get("dns", []):
    command.extend(("--dns", resolver))
for name in (*names, "PGOPTIONS", "PGCONNECT_TIMEOUT"):
    command.extend(("--env", name))
command.extend(("--mount", "type=bind,src=" + roots[0]["source"] + ",dst=" + source["PGSSLROOTCERT"] + ",readonly", "--entrypoint", "psql", services["holo-postgres"]["image"], "--no-psqlrc", "-v", "ON_ERROR_STOP=1", "-At", "-c", sys.argv[1]))
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
  (set -e; restore)
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
(cd "$staging" && sha256sum --check --strict image.tar.sha256 collector-image.tar.sha256)
docker load --input "$staging/image.tar"
docker load --input "$staging/collector-image.tar"
po_verify_image "$issuer" "$staging/rootfs-manifest.json" "$revision" arm64 "$staging/image-id"
verify_collector "$collector" "$staging/collector-manifest.json" "$revision" "$staging/collector-image-id"
[[ "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$issuer")" == "$version" ]]
[[ "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$collector")" == "$version" ]]
compose config --quiet
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf '%s\n' "$started_at" > "$backup/change-started-at"
compose up -d --no-build --no-deps --force-recreate "$issuer_container_service"
wait_issuer "$revision"
install -D -m 0644 "$staging/rootfs-manifest.json" "$current/$manifest"
install -D -m 0644 "$staging/image-id" "$current/$image_id_file"
compose up -d --no-build --no-deps --force-recreate youtube-collector
wait_collector "$revision"
since_epoch="$(date -u -d "$started_at" +%s)"
for container in "$issuer_container" "$collector_container"; do
  [[ "$(date -u -d "$(docker inspect -f '{{.State.StartedAt}}' "$container")" +%s)" -ge "$since_epoch" ]]
  require_post_cutover_logs "$started_at" "$container"
done
trap - ERR INT TERM HUP
printf 'central issuer-first cutover verified revision=%s backup=%s started_at=%s\n' "$revision" "$backup" "$started_at"
