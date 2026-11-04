#!/usr/bin/env bash
# 중앙(hololive-seoul) hololive-api·hololive-alarm-worker의 image 교체 helper다. Docker 시절
# compose-redeploy-service.sh가 보장하던 순서를 Quadlet drop-in으로 지킨다. collector c는 po-central-cutover.sh가 소유한다.
#  1. preflight: root, 배포 트리 revision(sync-opt-current.sh의 .source-revision)과 image revision label 일치,
#     image 존재·platform, primary fence 부재, unit 실행 중, PostgreSQL 연결 예산
#  2. 실행 중 이전 pair 보존(capture). drop-in이 없는 첫 도입은 prepare로 실행 중 image를 먼저 고정해 이후 자동
#     재시작이 tag 이동을 따라가지 않게 한다. 이어서 rollback drop-in과 이전 image의 --check-worker-profile
#  3. candidate pair·--check-worker-profile·verify-source
#  4. 활성화 전에 hololive-db-migrate의 image를 drop-in(50-migrator-image.conf)으로 고정하고 migration 실행
#  5. candidate drop-in 활성화, daemon-reload, restart(Notify=healthy가 health gate), verify-live·NRestarts
# 활성화를 시작한 뒤 실패하면 Docker 시절처럼 자동으로 되돌리지 않고 failed_after_activation으로 보고한다. 이전 pair
# 복원은 스키마·admission 호환성 승인 뒤 quadlet-rollback-service.sh --check·--apply로 한다. 어느 단계도 재시도하지 않는다.
set -Eeuo pipefail

die() { echo "[quadlet-redeploy] $*" >&2; exit 1; }
usage() {
  echo "Usage: $0 hololive-api <api-image>" >&2
  echo "       $0 hololive-alarm-worker <alarm-worker-image> <api-image-for-migration>" >&2
  echo "Images are sha256:<id> (build host podman image inspect .Id) or a loaded reference." >&2
  exit 2
}

service="${1:-}"
case "$service" in
  hololive-api)
    [[ $# -eq 2 ]] || usage
    candidate_ref="$2" migrator_ref="$2"
    ;;
  hololive-alarm-worker)
    # db-migrate binary와 migration runner는 API image에만 있다. 같은 revision의 API image로 실행한다.
    [[ $# -eq 3 ]] || usage
    candidate_ref="$2" migrator_ref="$3"
    ;;
  *) usage ;;
esac
unit="$service.service"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# 운영(root)은 경로를 고정하고 배포 트리의 사본만 실행한다. 테스트만 비root로 HOLOLIVE_QUADLET_TEST_ROOT 아래를 쓴다
# (quadlet-prestart.sh·worker-profile-pair.py와 같은 접두어).
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  [[ -z "${HOLOLIVE_QUADLET_TEST_ROOT:-}" ]] || die "HOLOLIVE_QUADLET_TEST_ROOT is only for non-root tests"
  prefix=""
else
  prefix="${HOLOLIVE_QUADLET_TEST_ROOT:-}"
  [[ -n "$prefix" ]] || die "must run as root (sudo)"
fi
deploy_root="$(realpath -m -- "$prefix/opt/hololive-bot/compose/current")"
quadlet_dir="$prefix/etc/containers/systemd/hololive-bot"
compose_env="$prefix/etc/stack-secrets/hololive-bot/compose.env"
fence_dir="$prefix/var/lib/hololive-postgres-fence"
if [[ -z "$prefix" && "$ROOT_DIR" != "$deploy_root" ]]; then
  die "run the deployed copy: $deploy_root/scripts/deploy/quadlet-redeploy-service.sh"
fi
pair_tool="$ROOT_DIR/scripts/deploy/lib/worker-profile-pair.py"
# api·alarm-worker helper가 함께 돌면 migrator drop-in 쓰기, daemon-reload, migration이 서로 끼어든다. 하나씩만 실행한다.
lock_dir="$prefix/run/lock"
install -d -m 0755 "$lock_dir"
exec 9>"$lock_dir/hololive-quadlet-redeploy.lock"
flock -n 9 || die "another quadlet-redeploy-service.sh is running; wait for its [OUTCOME] line"
# shellcheck source=scripts/deploy/lib/postgres-capacity.sh
. "$ROOT_DIR/scripts/deploy/lib/postgres-capacity.sh"

stage=preflight
release=""
activation_started=false
on_exit() {
  local status="$1"
  trap - EXIT INT TERM HUP
  [[ "$status" -ne 0 ]] || return 0
  if [[ "$activation_started" != true ]]; then
    echo "[OUTCOME] not_activated service=$service stage=$stage release=${release:-none}: the running pair was not replaced" >&2
  else
    # Docker 시절 compose-redeploy-service.sh처럼 자동으로 되돌리지 않는다. 이번 배포가 migration을 적용했으면
    # 이전 image가 새 스키마에서 틀리게 동작할 수 있다(예: hololive-api 런북의 migration 328 계약).
    echo "[OUTCOME] failed_after_activation service=$service stage=$stage release=$release: the candidate pair stays installed; roll back only after schema/admission compatibility is approved: scripts/deploy/quadlet-rollback-service.sh $release --check, then --apply" >&2
  fi
  exit "$status"
}
trap 'on_exit $?' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

# 알림 dispatcher는 중앙 하나뿐이다. sync-opt-current.sh와 같이 build-control host kapu에서는 거부한다.
if [[ "$(hostname -s)" == kapu && "${HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED:-}" != 1 ]]; then
  die "refusing to redeploy central units on build-control host kapu"
fi
# 모든 중앙 unit은 fence 표식이 있으면 기동하지 않는다(ConditionPathExists=!).
assert_no_fence_marker() {
  local marker
  for marker in fence.intent fenced; do
    [[ ! -e "$fence_dir/$marker" ]] || die "primary fence marker present: $fence_dir/$marker"
  done
}
assert_no_fence_marker
[[ -f "$quadlet_dir/$service.container" ]] || die "installed unit missing: $quadlet_dir/$service.container"
# Quadlet은 정지한 unit의 container를 지운다. 이전 profile은 실행 중인 process에서만 증명할 수 있다.
systemctl is-active --quiet "$unit" || die "$unit must be running so its loaded profile pair can be retained"

[[ -f "$deploy_root/.source-revision" ]] ||
  die "deploy tree revision record missing; run sync-opt-current.sh from the clean checkout first"
revision="$(<"$deploy_root/.source-revision")"
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || die "deploy tree revision is not a full commit: $deploy_root/.source-revision"
host_arch="$(podman info --format '{{.Host.Arch}}')"

# 이후 조회·고정은 identity로 한다. 그 사이 tag가 옮겨져도 검사한 image와 고정하는 image가 같다.
resolve_image() {
  local ref="$1" title="$2" status=0 id value
  podman image exists "$ref" || status="$?"
  case "$status" in
    0) ;;
    1) die "image not found in the root store: $ref" ;;
    *) die "image inventory failed for $ref (status=$status)" ;;
  esac
  id="$(podman image inspect --format '{{.Id}}' "$ref")"
  [[ "$id" =~ ^[0-9a-f]{64}$ ]] || die "unexpected image identity for $ref"
  id="sha256:$id"
  value="$(podman image inspect --format '{{.Os}}/{{.Architecture}}' "$id")"
  [[ "$value" == "linux/$host_arch" ]] || die "$ref is $value; this host runs linux/$host_arch"
  value="$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.title"}}' "$id")"
  [[ "$value" == "$title" ]] || die "$ref is not a $title image"
  value="$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$id")"
  [[ "$value" == "$revision" ]] || die "$ref revision label does not match the deploy tree revision $revision"
  printf '%s\n' "$id"
}
candidate_id="$(resolve_image "$candidate_ref" "$service")"
migrator_id="$(resolve_image "$migrator_ref" hololive-api)"
# Docker 시절 compose.sh가 기동마다 하던 연결 예산 검사다. 출력은 key 이름과 합계만 담는다.
postgres_capacity_assert_target "$ROOT_DIR" "$compose_env"
echo "[INFO] service=$service revision=$revision candidate=$candidate_id migrator=$migrator_id"

stage=previous-pair
releases="$deploy_root/artifacts/worker-profile/releases"
install -d -m 0755 "$releases"
release="$(mktemp -d "$releases/$service-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")"
chmod 0755 "$release"
printf '%s\n' "$service" >"$release/service"
printf '%s\n' "$revision" >"$release/candidate.revision"
if [[ -e "$quadlet_dir/$service.container.d/50-worker-profile.conf" ]]; then
  python3 "$pair_tool" capture "$service" "$release/previous" "$service" "$deploy_root"
else
  python3 "$pair_tool" prepare "$service" "$release/previous" "$service" "$deploy_root"
  systemctl daemon-reload
  echo "[PREPARE] $service pinned to its running image before the change: $release/previous"
fi
python3 "$pair_tool" rollback "$service" "$release/previous"
python3 "$pair_tool" check "$service" "$release/previous"
echo "[ROLLBACK] $release (schema/admission compatibility must be approved before a later manual apply)"

stage=candidate-pair
python3 "$pair_tool" candidate "$service" "$release/candidate" "$candidate_id" <"$quadlet_dir/$service.container"
python3 "$pair_tool" check "$service" "$release/candidate"
python3 "$pair_tool" verify-source "$service" "$release/candidate"

stage=migration
# Quadlet drop-in의 Image=는 unit의 Image=${HOLOLIVE_API_IMAGE}(compose.env 값 포함)를 대신한다. 같은 drop-in은
# 다음 배포가 바꿀 때까지 남아 수동 `systemctl start hololive-db-migrate.service`도 같은 runner를 쓴다.
migrate_dropins="$quadlet_dir/hololive-db-migrate.container.d"
install -d -m 0755 "$migrate_dropins"
migrate_tmp="$(mktemp "$migrate_dropins/.50-migrator-image.conf.XXXXXX")"
printf '%s\n' \
  "# quadlet-redeploy-service.sh가 생성한 migrator image 고정 drop-in이다. 직접 고치지 않는다." \
  "[Container]" \
  "Image=$migrator_id" >"$migrate_tmp"
chmod 0644 "$migrate_tmp"
mv -f -- "$migrate_tmp" "$migrate_dropins/50-migrator-image.conf"
printf '%s\n' "$migrator_id" >"$release/migrator.image"
systemctl daemon-reload
# fence 조건으로 건너뛴 oneshot도 start는 0으로 끝나고, 끝난 oneshot은 unload될 수 있어 ConditionResult로 확인할 수 없다.
# 표식이 start 전후 모두 없으면 조건을 통과해 실제로 실행된 것이다. 사이에 생겼으면 결과를 알 수 없으므로 멈춘다.
assert_no_fence_marker
systemctl start hololive-db-migrate.service
assert_no_fence_marker
echo "[MIGRATE] hololive-db-migrate completed with $migrator_id"

stage=activation
activation_started=true
python3 "$pair_tool" activate "$service" "$release/candidate" "$deploy_root"
systemctl daemon-reload
restarts_before="$(systemctl show "$unit" -p NRestarts --value)"
printf '%s\n' "$restarts_before" >"$release/nrestarts-before"
echo "[RESTART] $unit (NRestarts before=$restarts_before)"
# Notify=healthy라 restart는 candidate가 healthy가 된 뒤에만 성공한다(TimeoutStartSec 안).
systemctl restart "$unit"

stage=verify
systemctl is-active --quiet "$unit" || die "$unit is not active after restart"
python3 "$pair_tool" verify-live "$service" "$release/candidate" "$service"
# 명시적 restart는 NRestarts를 0으로 되돌린다(systemd 257 실측). 0이 아니면 그 사이 자동 재시작(unhealthy 정지·crash)이 있었다.
restarts="$(systemctl show "$unit" -p NRestarts --value)"
[[ "$restarts" == 0 ]] || die "$unit restarted automatically after the change (NRestarts=$restarts)"
echo "[OUTCOME] deployed service=$service revision=$revision image=$candidate_id release=$release"
