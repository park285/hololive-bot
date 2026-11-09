#!/usr/bin/env bash
# 중앙(hololive-seoul) hololive-api·hololive-alarm-worker의 image 교체 helper다. 아래 순서를 Quadlet drop-in으로
# 지킨다. collector c는 po-central-cutover.sh가 소유한다.
#  1. preflight: root, 배포 트리 revision(sync-opt-current.sh의 .source-revision)과 image revision label 일치,
#     image 존재·platform, primary fence 부재, unit 실행 중, PostgreSQL 연결 예산
#  2. 실행 중 이전 pair 보존(capture). drop-in이 없는 첫 도입은 prepare로 실행 중 image를 먼저 고정해 이후 자동
#     재시작이 tag 이동을 따라가지 않게 한다. 이어서 rollback drop-in과 이전 image의 --check-worker-profile
#  3. candidate pair·--check-worker-profile·verify-source
#  4. 활성화 전에 hololive-db-migrate의 image를 drop-in(50-migrator-image.conf)으로 고정하고 migration 실행
#  5. candidate drop-in 활성화, daemon-reload, restart(Notify=healthy가 health gate), verify-live·NRestarts
# 활성화를 시작한 뒤 실패하면 자동으로 되돌리지 않고 failed_after_activation으로 보고한다. 이전 pair
# 복원은 스키마·admission 호환성 승인 뒤 quadlet-rollback-service.sh --check·--apply로 한다. 어느 단계도 재시도하지 않는다.
# --quiesce는 두 Iris 소비자의 pair를 보존한 뒤 함께 정지·영속 mask한다. 외부 Iris 전환 뒤
# --resume은 같은 release를 재검증하고 두 후보를 모두 설치한 다음에만 mask를 해제한다.
set -Eeuo pipefail

die() { echo "[quadlet-redeploy] $*" >&2; exit 1; }
usage() {
  echo "Usage: $0 hololive-api <api-image>" >&2
  echo "       $0 hololive-alarm-worker <alarm-worker-image> <api-image-for-migration>" >&2
  echo "       $0 --quiesce <api-image> <alarm-worker-image>" >&2
  echo "       $0 --resume <api-release-dir> <alarm-worker-release-dir>" >&2
  echo "Images are sha256:<id> (build host podman image inspect .Id) or a loaded reference." >&2
  exit 2
}

mode=deploy
declare -A candidate_refs=() candidate_ids=() release_paths=()
case "${1:-}" in
  hololive-api)
    [[ $# -eq 2 ]] || usage
    services=(hololive-api)
    candidate_refs[hololive-api]="$2"
    migrator_ref="$2"
    ;;
  hololive-alarm-worker)
    [[ $# -eq 3 ]] || usage
    services=(hololive-alarm-worker)
    candidate_refs[hololive-alarm-worker]="$2"
    migrator_ref="$3"
    ;;
  --quiesce)
    [[ $# -eq 3 ]] || usage
    mode=quiesce
    services=(hololive-alarm-worker hololive-api)
    candidate_refs[hololive-api]="$2"
    candidate_refs[hololive-alarm-worker]="$3"
    migrator_ref="$2"
    ;;
  --resume)
    [[ $# -eq 3 ]] || usage
    mode=resume
    services=(hololive-alarm-worker hololive-api)
    release_paths[hololive-api]="$2"
    release_paths[hololive-alarm-worker]="$3"
    ;;
  *) usage ;;
esac
service="${services[0]}"
units=()
for service in "${services[@]}"; do
  units+=("$service.service")
done

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
releases="$deploy_root/artifacts/worker-profile/releases"
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
quiescence_started=false
on_exit() {
  local status="$1"
  trap - EXIT INT TERM HUP
  [[ "$status" -ne 0 ]] || return 0
  if [[ "$activation_started" == true ]]; then
    if [[ "$mode" == deploy ]]; then
      echo "[OUTCOME] failed_after_activation service=$service stage=$stage release=$release: the candidate pair stays installed; roll back only after schema/admission compatibility is approved: scripts/deploy/quadlet-rollback-service.sh $release --check, then --apply" >&2
    else
      echo "[OUTCOME] failed_after_activation services=${services[*]} stage=$stage releases=${release_paths[*]:-none}: retain all pairs and inspect unit state before an approved rollback" >&2
    fi
  elif [[ "$quiescence_started" == true ]]; then
    echo "[OUTCOME] quiescence_failed services=${services[*]} stage=$stage releases=${release_paths[*]:-none}: the hold may be partial; inspect both units and do not unmask automatically" >&2
  else
    echo "[OUTCOME] not_activated service=$service stage=$stage release=${release:-none}: the candidate pair was not installed" >&2
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
assert_coordinated_hold() {
  local held_unit
  for held_unit in "${units[@]}"; do
    [[ "$(systemctl show "$held_unit" -p LoadState --value)" == masked &&
      "$(systemctl show "$held_unit" -p UnitFileState --value)" == masked &&
      "$(systemctl show "$held_unit" -p ActiveState --value)" == inactive ]] ||
      die "$held_unit must remain persistently masked and inactive for coordinated resume"
  done
}
if [[ "$mode" == resume ]]; then
  assert_coordinated_hold
  quiescence_started=true
else
  for service in "${services[@]}"; do
    [[ -f "$quadlet_dir/$service.container" ]] || die "installed unit missing: $quadlet_dir/$service.container"
    # Quadlet은 정지할 때 container를 지우므로 이전 loaded profile을 먼저 보존한다.
    systemctl is-active --quiet "$service.service" ||
      die "$service.service must be running so its loaded profile pair can be retained"
    if [[ "$mode" == quiesce ]]; then
      [[ "$(systemctl show "$service.service" -p LoadState --value)" == loaded ]] ||
        die "$service.service already has a hold that this operation does not own"
    fi
  done
fi

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
load_coordinated_release() {
  local requested="$1" field
  [[ "$requested" == /* ]] || die "release path must be absolute"
  release="$(realpath -e -- "$requested")" || die "release directory not found"
  [[ -d "$release" && "$release" == "$releases/"* ]] || die "release escaped the retained store"
  for field in service candidate.revision candidate.image migrator.image coordinated.services; do
    [[ -f "$release/$field" && ! -L "$release/$field" ]] || die "retained release metadata is missing: $field"
  done
  cmp -s "$release/service" <(printf '%s\n' "$service") || die "retained release service mismatch"
  cmp -s "$release/candidate.revision" <(printf '%s\n' "$revision") || die "deployment tree changed after quiescence"
  cmp -s "$release/coordinated.services" <(printf '%s\n' "${services[@]}") || die "release was not prepared for both Iris consumers"
  candidate_refs[$service]="$(<"$release/candidate.image")"
  [[ "${candidate_refs[$service]}" =~ ^sha256:[0-9a-f]{64}$ ]] &&
    cmp -s "$release/candidate.image" <(printf '%s\n' "${candidate_refs[$service]}") ||
    die "retained candidate identity is invalid"
  release_paths[$service]="$release"
}

if [[ "$mode" == resume ]]; then
  for service in "${services[@]}"; do
    load_coordinated_release "${release_paths[$service]}"
  done
  migrator_ref="${candidate_refs[hololive-api]}"
fi
for service in "${services[@]}"; do
  candidate_ids[$service]="$(resolve_image "${candidate_refs[$service]}" "$service")"
done
migrator_id="$(resolve_image "$migrator_ref" hololive-api)"
postgres_capacity_assert_target "$ROOT_DIR" "$compose_env"

prepare_slot() {
  stage=previous-pair
  install -d -m 0755 "$releases"
  release="$(mktemp -d "$releases/$service-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")"
  release_paths[$service]="$release"
  chmod 0755 "$release"
  printf '%s\n' "$service" >"$release/service"
  printf '%s\n' "$revision" >"$release/candidate.revision"
  printf '%s\n' "${candidate_ids[$service]}" >"$release/candidate.image"
  printf '%s\n' "$migrator_id" >"$release/migrator.image"
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
  python3 "$pair_tool" candidate "$service" "$release/candidate" "${candidate_ids[$service]}" <"$quadlet_dir/$service.container"
  python3 "$pair_tool" check "$service" "$release/candidate"
  python3 "$pair_tool" verify-source "$service" "$release/candidate"
}

for service in "${services[@]}"; do
  echo "[INFO] service=$service revision=$revision candidate=${candidate_ids[$service]} migrator=$migrator_id"
  if [[ "$mode" == resume ]]; then
    release="${release_paths[$service]}"
    cmp -s "$release/migrator.image" <(printf '%s\n' "$migrator_id") || die "retained migrator identity mismatch"
    python3 "$pair_tool" verify "$service" "$release/candidate" "${candidate_ids[$service]}"
    python3 "$pair_tool" verify-source "$service" "$release/candidate"
  else
    prepare_slot
  fi
  if [[ "$mode" != deploy ]]; then
    python3 "$pair_tool" verify-retained "$service" "$release/previous" "$release/candidate" "$deploy_root"
  fi
done

if [[ "$mode" == quiesce ]]; then
  stage=quiescence
  for service in "${services[@]}"; do
    release="${release_paths[$service]}"
    python3 "$pair_tool" verify-live "$service" "$release/previous" "$service"
    printf '%s\n' "${services[@]}" >"$release/coordinated.services"
  done
  quiescence_started=true
  systemctl mask --now "${units[@]}"
  assert_coordinated_hold
  echo "[OUTCOME] quiesced api_release=${release_paths[hololive-api]} alarm_release=${release_paths[hololive-alarm-worker]}: migrate and activate only with explicit --resume after the coordinated peer cutover"
  exit 0
fi

stage=migration
migrate_dropins="$quadlet_dir/hololive-db-migrate.container.d"
install -d -m 0755 "$migrate_dropins"
migrate_tmp="$(mktemp "$migrate_dropins/.50-migrator-image.conf.XXXXXX")"
printf '%s\n' \
  "# quadlet-redeploy-service.sh가 생성한 migrator image 고정 drop-in이다. 직접 고치지 않는다." \
  "[Container]" \
  "Image=$migrator_id" >"$migrate_tmp"
chmod 0644 "$migrate_tmp"
mv -f -- "$migrate_tmp" "$migrate_dropins/50-migrator-image.conf"
systemctl daemon-reload
assert_no_fence_marker
systemctl start hololive-db-migrate.service
assert_no_fence_marker
echo "[MIGRATE] hololive-db-migrate completed with $migrator_id"

stage=activation
[[ "$mode" != resume ]] || assert_coordinated_hold
if [[ "$mode" == resume ]]; then
  for service in "${services[@]}"; do
    release="${release_paths[$service]}"
    python3 "$pair_tool" verify-source "$service" "$release/candidate"
    python3 "$pair_tool" verify-retained "$service" "$release/previous" "$release/candidate" "$deploy_root"
  done
fi
activation_started=true
for service in "${services[@]}"; do
  release="${release_paths[$service]}"
  python3 "$pair_tool" activate "$service" "$release/candidate" "$deploy_root"
done
systemctl daemon-reload
if [[ "$mode" == resume ]]; then
  # 두 drop-in을 모두 게시하기 전에는 API의 Wants가 구형 worker를 기동하지 못하게 한다.
  systemctl unmask "${units[@]}"
fi
for service in "${services[@]}"; do
  release="${release_paths[$service]}"
  stage=activation
  unit="$service.service"
  restarts_before="$(systemctl show "$unit" -p NRestarts --value)"
  printf '%s\n' "$restarts_before" >"$release/nrestarts-before"
  echo "[RESTART] $unit (NRestarts before=$restarts_before)"
  # worker를 먼저 기동하고 API를 뒤에 둬 Wants 관계에서도 같은 후보 쌍만 실행한다.
  systemctl restart "$unit"
  stage=verify
  systemctl is-active --quiet "$unit" || die "$unit is not active after restart"
  python3 "$pair_tool" verify-live "$service" "$release/candidate" "$service"
  restarts="$(systemctl show "$unit" -p NRestarts --value)"
  [[ "$restarts" == 0 ]] || die "$unit restarted automatically after the change (NRestarts=$restarts)"
  echo "[OUTCOME] deployed service=$service revision=$revision image=${candidate_ids[$service]} release=$release"
done
