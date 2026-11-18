#!/usr/bin/env bash
# 중앙(hololive-seoul) hololive-api·hololive-alarm-worker의 image 교체 helper다. 아래 순서를 Quadlet drop-in으로
# 지킨다. collector c는 po-central-cutover.sh가 소유한다.
#  1. preflight: root, 배포 트리 revision(sync-opt-current.sh의 .source-revision)과 image revision label 일치,
#     image 존재·platform, primary fence 부재, unit 실행 중, PostgreSQL 연결 예산·실행 중 풀러 설정
#  2. 실행 중 이전 pair 보존(capture). drop-in이 없는 첫 도입은 prepare로 실행 중 image를 먼저 고정해 이후 자동
#     재시작이 tag 이동을 따라가지 않게 한다. 이어서 rollback drop-in과 이전 image의 --check-worker-profile
#  3. candidate pair·--check-worker-profile·verify-source
#  4. 활성화 전에 hololive-db-migrate의 image를 drop-in(50-migrator-image.conf)으로 고정하고 migration 실행
#  5. candidate drop-in 활성화, daemon-reload, restart(Notify=healthy가 health gate), verify-live·NRestarts
# 활성화를 시작한 뒤 실패하면 자동으로 되돌리지 않고 failed_after_activation으로 보고한다. 이전 pair
# 복원은 스키마·admission 호환성 승인 뒤 quadlet-rollback-service.sh --check·--apply로 한다. 어느 단계도 재시도하지 않는다.
# --quiesce는 현재 세대의 두 writer를 한 작업의 영속 mask로 정지한다. 최초 endpoint 전환은
# 검토한 staged archive의 --quiesce-before-sync로 기존 unit을 바꾸기 전에 정지하고 후보 준비는 sync 뒤로 미룬다.
# --resume은 migration과 역할별 pooled SQL 검증 뒤 두 후보를 설치하고 worker를 확인한 뒤에만 API를 연다.
set -Eeuo pipefail

die() { echo "[quadlet-redeploy] $*" >&2; exit 1; }
usage() {
  echo "Usage: $0 hololive-api <api-image>" >&2
  echo "       $0 hololive-alarm-worker <alarm-worker-image> <api-image-for-migration>" >&2
  echo "       $0 --quiesce <api-image> <alarm-worker-image>" >&2
  echo "       $0 --quiesce-before-sync <api-image> <alarm-worker-image> sha256:<pooler-id>   (verified staged archive only)" >&2
  echo "       $0 --resume <api-release-dir> <alarm-worker-release-dir>" >&2
  echo "Images are sha256:<id> (build host podman image inspect .Id) or a loaded reference." >&2
  exit 2
}

mode=deploy
before_sync=false
declare -A candidate_refs=() candidate_ids=() release_paths=() original_invocations=()
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
  --quiesce|--quiesce-before-sync)
    if [[ "$1" == --quiesce-before-sync ]]; then
      [[ $# -eq 4 && "$4" =~ ^sha256:[0-9a-f]{64}$ ]] || usage
      before_sync=true
      pooler_id="$4"
    else
      [[ $# -eq 3 ]] || usage
    fi
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
source_stamp="$deploy_root/.source-revision"
if [[ "$before_sync" == true ]]; then
  source_stamp="$ROOT_DIR/.source-revision"
  [[ "$ROOT_DIR" == "$prefix/opt/hololive-bot/compose/staged/${ROOT_DIR##*/}" &&
    "${ROOT_DIR##*/}" =~ ^[0-9a-f]{40}$ && -f "$source_stamp" && ! -L "$source_stamp" &&
    "$(<"$source_stamp")" == "${ROOT_DIR##*/}" ]] || die "run the verified revision-named staged archive before sync"
  installed_revision="$(<"$deploy_root/.source-revision")"
  [[ "$installed_revision" =~ ^[0-9a-f]{40}$ && "$installed_revision" != "${ROOT_DIR##*/}" ]] ||
    die "pre-sync hold requires the prior installed deployment revision"
  if [[ -z "$prefix" ]]; then
    bash "$ROOT_DIR/scripts/deploy/verify-exec-tree-ownership.sh" "$source_stamp" "${BASH_SOURCE[0]}" \
      "$ROOT_DIR/scripts/deploy/lib/"{worker-profile-pair.py,postgres-capacity.sh,postgres-endpoint.sh,compose-env.sh} \
      "$ROOT_DIR/scripts/ci/postgres-capacity-policy.tsv"
  fi
elif [[ -z "$prefix" && "$ROOT_DIR" != "$deploy_root" ]]; then
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
. "$ROOT_DIR/scripts/deploy/lib/compose-env.sh"
. "$ROOT_DIR/scripts/deploy/lib/postgres-endpoint.sh"
mask_dir="$prefix/etc/systemd/system"
sync_paths() {
  python3 - "$@" <<'PY'
import os
import sys

for path in sys.argv[1:]:
    fd = os.open(path, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
PY
}
opened=()

stage=preflight
release=""
activation_started=false
quiescence_started=false
on_exit() {
  local status="$1"
  trap - EXIT INT TERM HUP
  [[ "$status" -ne 0 ]] || return 0
  if (( ${#opened[@]} )); then
    if (restore_opened_hold); then
      echo "[HOLD] only the units opened by this resume were stopped and re-masked" >&2
    else
      echo "[HOLD] recovery unconfirmed; inspect both writer units without retrying" >&2
    fi
  fi
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
mask_path() { printf '%s/%s.service' "$mask_dir" "$1"; }
mask_anchor() { local dir="${release_paths[$1]}"; printf '%s/.%s.mask' "$mask_dir" "${dir##*/}"; }
owned_mask() {
  local path anchor owner installed
  path="$(mask_path "$1")"
  anchor="$(mask_anchor "$1")"
  [[ -L "$anchor" && -L "$path" && "$(readlink -- "$anchor")" == /dev/null ]] || return 1
  owner="$(stat -c '%d:%i' -- "$anchor")" && installed="$(stat -c '%d:%i' -- "$path")" &&
    [[ "$owner" == "$installed" ]]
}
assert_stopped() {
  local name="$1" unit="$1.service" rc=0
  owned_mask "$name" &&
    [[ "$(systemctl show "$unit" -p LoadState --value)" == masked &&
      "$(systemctl show "$unit" -p UnitFileState --value)" == masked &&
      "$(systemctl show "$unit" -p ActiveState --value)" == inactive &&
      "$(systemctl show "$unit" -p MainPID --value)" == 0 &&
      "$(systemctl show "$unit" -p ControlPID --value)" == 0 &&
      "$(systemctl show "$unit" -p Result --value)" == success ]] || return 1
  podman container exists "$name" || rc=$?
  [[ "$rc" == 1 ]]
}
assert_coordinated_hold() {
  local name
  for name in "${services[@]}"; do
    assert_stopped "$name" || die "$name: owned persistent writer hold not confirmed"
  done
}
restore_opened_hold() {
  local name path anchor state failed=false
  for name in "${opened[@]}"; do
    path="$(mask_path "$name")"
    anchor="$(mask_anchor "$name")"
    if [[ ! -e "$path" && ! -L "$path" ]]; then
      ln -PT -- "$anchor" "$path" || failed=true
    fi
    owned_mask "$name" || failed=true
  done
  sync_paths "$mask_dir" || failed=true
  systemctl daemon-reload || return 1
  for name in "${opened[@]}"; do
    if ! owned_mask "$name"; then failed=true; continue; fi
    state="$(systemctl show "$name.service" -p ActiveState --value)" || { failed=true; continue; }
    case "$state" in
      inactive) ;;
      active|activating|deactivating|reloading) systemctl stop "$name.service" || failed=true ;;
      *) failed=true ;;
    esac
  done
  [[ "$failed" == false ]] || return 1
  assert_coordinated_hold
}
if [[ "$mode" != resume ]]; then
  for service in "${services[@]}"; do
    [[ -f "$quadlet_dir/$service.container" ]] || die "installed unit missing: $quadlet_dir/$service.container"
    # Quadlet은 정지할 때 container를 지우므로 이전 loaded profile을 먼저 보존한다.
    systemctl is-active --quiet "$service.service" ||
      die "$service.service must be running so its loaded profile pair can be retained"
    if [[ "$mode" == quiesce ]]; then
      [[ "$(systemctl show "$service.service" -p LoadState --value)" == loaded &&
        ! -e "$(mask_path "$service")" && ! -L "$(mask_path "$service")" ]] ||
        die "$service.service already has a hold that this operation does not own"
      invocation="$(systemctl show "$service.service" -p InvocationID --value)"
      [[ "$invocation" =~ ^[0-9a-f]{32}$ && "$invocation" != 00000000000000000000000000000000 ]] ||
        die "$service has no running invocation"
      original_invocations[$service]="$invocation"
    fi
  done
fi

[[ -f "$source_stamp" ]] ||
  die "deploy tree revision record missing; run sync-opt-current.sh from the clean checkout first"
revision="$(<"$source_stamp")"
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
  for field in service candidate.revision candidate.image migrator.image previous.endpoint captured.invocation; do
    [[ -f "$release/$field" && ! -L "$release/$field" ]] || die "retained release metadata is missing: $field"
  done
  cmp -s "$release/service" <(printf '%s\n' "$service") || die "retained release service mismatch"
  cmp -s "$release/candidate.revision" <(printf '%s\n' "$revision") || die "deployment tree changed after quiescence"
  candidate_refs[$service]="$(<"$release/candidate.image")"
  [[ "${candidate_refs[$service]}" =~ ^sha256:[0-9a-f]{64}$ ]] &&
    cmp -s "$release/candidate.image" <(printf '%s\n' "${candidate_refs[$service]}") ||
    die "retained candidate identity is invalid"
  release_paths[$service]="$release"
}

hold_receipt() {
  local name dir invocation inode endpoint
  printf '%s\n' "$before_sync"
  for name in "${services[@]}"; do
    dir="${release_paths[$name]}"
    invocation="$(<"$dir/captured.invocation")"
    [[ "$invocation" =~ ^[0-9a-f]{32}$ ]] || return 1
    inode="$(stat -c %i -- "$(mask_anchor "$name")")" || return 1
    endpoint="$(<"$dir/previous.endpoint")"
    [[ -n "$endpoint" && "$endpoint" != *$'\n'* ]] || return 1
    printf '%s %s %s %s %s\n' "$name" "$dir" "$invocation" "$inode" "$endpoint"
    (cd "$dir" && sha256sum previous/pair.json candidate.image migrator.image candidate.revision) || return 1
    if [[ "$before_sync" == true ]]; then
      (cd "$dir" && sha256sum pooler.image pooler.platform pooler.revision) || return 1
    fi
  done
}
check_receipt() {
  local receipt="${release_paths[hololive-api]}/quiesced" expected
  [[ -f "$receipt" && ! -L "$receipt" ]] && IFS= read -r before_sync <"$receipt" &&
    [[ "$before_sync" == true || "$before_sync" == false ]] || die "missing or invalid quiescence receipt"
  expected="$(hold_receipt)" && cmp -s "$receipt" <(printf '%s\n' "$expected") ||
    die "release pair or mask does not belong to this quiescence receipt"
  assert_coordinated_hold
}

if [[ "$mode" == resume ]]; then
  for service in "${services[@]}"; do
    load_coordinated_release "${release_paths[$service]}"
  done
  check_receipt
  quiescence_started=true
  migrator_ref="${candidate_refs[hololive-api]}"
  if [[ "$before_sync" == true ]]; then
    pooler_id="$(<"${release_paths[hololive-api]}/pooler.image")"
    for service in "${services[@]}"; do
      release="${release_paths[$service]}"
      cmp -s "$release/pooler.image" <(printf '%s\n' "$pooler_id") &&
        cmp -s "$release/pooler.platform" <(printf 'linux/%s\n' "$host_arch") &&
        cmp -s "$release/pooler.revision" <(printf '%s\n' "$revision") ||
        die "$service retained pooler identity does not match the staged release"
    done
  fi
fi
assert_running_pooler_image() {
  [[ "$before_sync" == true ]] || return 0
  local actual
  actual="$(podman container inspect --format '{{.State.Running}} {{.Image}}' holo-pgbouncer)"
  [[ "$actual" == "true ${pooler_id#sha256:}" ]] || die "running pooler differs from the staged image"
}
if [[ "$before_sync" == true ]]; then
  [[ "$pooler_id" =~ ^sha256:[0-9a-f]{64}$ && "$host_arch" == arm64 ]] &&
    [[ "$(resolve_image "$pooler_id" iris-stack-pgbouncer)" == "$pooler_id" ]] ||
    die "staged pooler identity, platform or revision mismatch"
  if [[ "$mode" == resume ]]; then assert_running_pooler_image; fi
fi
for service in "${services[@]}"; do
  candidate_ids[$service]="$(resolve_image "${candidate_refs[$service]}" "$service")"
done
migrator_id="$(resolve_image "$migrator_ref" hololive-api)"
postgres_capacity_assert_target "$ROOT_DIR" "$compose_env"
assert_writer_endpoint() {
  local unit="$quadlet_dir/$1.container" values
  values="$(postgres_quadlet_environment "$unit" "$compose_env" "$unit" POSTGRES_HOST POSTGRES_PORT POSTGRES_SOCKET_PATH POSTGRES_SSLMODE)"
  [[ "$values" == $'POSTGRES_HOST=holo-pgbouncer\nPOSTGRES_PORT=6432\nPOSTGRES_SOCKET_PATH=\nPOSTGRES_SSLMODE=verify-full' ]] ||
    die "$1: pooled TCP endpoint and verify-full are required"
}
if [[ "$mode" != quiesce ]]; then
  postgres_capacity_assert_live "$ROOT_DIR" "$compose_env" "$quadlet_dir"
  for service in "${services[@]}"; do assert_writer_endpoint "$service"; done
  if [[ "$mode" == deploy ]]; then
    postgres_endpoint_require_same "$(postgres_endpoint_container "$service")" 'holo-pgbouncer|6432' ||
      die "single-writer endpoint change refused; use the coordinated pre-sync hold"
  fi
fi

prepare_slot() {
  stage=previous-pair
  install -d -m 0755 "$releases"
  release="$(mktemp -d "$releases/$service-v2-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")"
  release_paths[$service]="$release"
  chmod 0755 "$release"
  printf '%s\n' "$service" >"$release/service"
  printf '%s\n' "$revision" >"$release/candidate.revision"
  printf '%s\n' "${candidate_ids[$service]}" >"$release/candidate.image"
  printf '%s\n' "$migrator_id" >"$release/migrator.image"
  if [[ "$before_sync" == true ]]; then
    printf '%s\n' "$pooler_id" >"$release/pooler.image"
    printf 'linux/%s\n' "$host_arch" >"$release/pooler.platform"
    printf '%s\n' "$revision" >"$release/pooler.revision"
  fi
  if [[ "$mode" == quiesce ]]; then
    printf '%s\n' "${original_invocations[$service]}" >"$release/captured.invocation"
  fi
  if [[ -e "$quadlet_dir/$service.container.d/50-worker-profile.conf" ]]; then
    python3 "$pair_tool" capture "$service" "$release/previous" "$service" "$deploy_root"
  else
    python3 "$pair_tool" prepare "$service" "$release/previous" "$service" "$deploy_root"
    systemctl daemon-reload
    echo "[PREPARE] $service pinned to its running image before the change: $release/previous"
  fi
  if [[ "$before_sync" == true ]]; then
    cmp -s "$release/previous/unit.container" "$quadlet_dir/$service.container" ||
      die "$service installed base unit differs from its retained running base"
  fi
  local captured_id endpoint
  captured_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["container_id"])' "$release/previous/pair.json")"
  [[ "$captured_id" =~ ^[0-9a-f]{64}$ ]] || die "$service has no captured container ID"
  endpoint="$(postgres_endpoint_container "$captured_id")"
  printf '%s\n' "$endpoint" >"$release/previous.endpoint"
  postgres_endpoint_require_same "$endpoint" "$(postgres_endpoint_quadlet "$quadlet_dir/$service.container" "$compose_env")" ||
    die "$service installed endpoint differs from the captured running generation"
  python3 "$pair_tool" rollback "$service" "$release/previous" "$release/rollback.conf"
  (cd "$release" && sha256sum previous/pair.json previous.endpoint rollback.conf >rollback.sha256)
  python3 "$pair_tool" check "$service" "$release/previous"
  echo "[ROLLBACK] $release (schema/admission compatibility must be approved before a later manual apply)"
}
prepare_candidate() {
  stage=candidate-pair
  python3 "$pair_tool" candidate "$service" "$release/candidate" "${candidate_ids[$service]}" <"$quadlet_dir/$service.container"
  python3 "$pair_tool" check "$service" "$release/candidate"
  python3 "$pair_tool" verify-source "$service" "$release/candidate"
}

for service in "${services[@]}"; do
  echo "[INFO] service=$service revision=$revision candidate=${candidate_ids[$service]} migrator=$migrator_id"
  if [[ "$mode" == resume ]]; then
    release="${release_paths[$service]}"
    if [[ "$before_sync" == true ]]; then
      cmp -s "$deploy_root/deploy/quadlet/hololive-bot/$service.container" "$quadlet_dir/$service.container" ||
        die "$service staged base unit is not installed"
      if [[ ! -e "$release/candidate" ]]; then
        prepare_candidate
      else
        python3 "$pair_tool" check "$service" "$release/candidate"
      fi
    fi
    cmp -s "$release/migrator.image" <(printf '%s\n' "$migrator_id") || die "retained migrator identity mismatch"
    python3 "$pair_tool" verify "$service" "$release/candidate" "${candidate_ids[$service]}"
    python3 "$pair_tool" verify-source "$service" "$release/candidate"
  else
    prepare_slot
    if [[ "$before_sync" != true ]]; then prepare_candidate; fi
  fi
  if [[ "$mode" == resume || ( "$mode" == quiesce && "$before_sync" != true ) ]]; then
    python3 "$pair_tool" verify-retained "$service" "$release/previous" "$release/candidate" "$deploy_root"
  fi
done

if [[ "$mode" == quiesce ]]; then
  stage=quiescence
  verify_original() {
    local name="$1" dir="${release_paths[$1]}"
    python3 "$pair_tool" verify-captured "$name" "$dir/previous" "$name"
    [[ "$(systemctl show "$name.service" -p ActiveState --value)" == active &&
      "$(systemctl show "$name.service" -p InvocationID --value)" == "$(<"$dir/captured.invocation")" ]] ||
      die "$name changed invocation after capture"
  }
  for service in "${services[@]}"; do verify_original "$service"; done
  quiescence_started=true
  # 같은 파일시스템의 별도 hard link가 mask inode의 재사용과 외부 mask의 인수를 막는다.
  for service in "${services[@]}"; do
    anchor="$(mask_anchor "$service")"
    ln -sT -- /dev/null "$anchor"
    ln -PT -- "$anchor" "$(mask_path "$service")"
  done
  sync_paths "$mask_dir"
  systemctl daemon-reload
  for service in "${services[@]}"; do
    owned_mask "$service" && [[ "$(systemctl show "$service.service" -p LoadState --value)" == masked ]] ||
      die "$service: persistent mask was not loaded"
    verify_original "$service"
  done
  systemctl stop "${units[@]}"
  for service in "${services[@]}"; do
    stopped_invocation="$(systemctl show "$service.service" -p InvocationID --value)"
    # mask가 반영된 뒤 실행 세대를 대조했다. inactive unit이 수거되면 manager의 ID는 비지만 다른 ID일 수는 없다.
    [[ -z "$stopped_invocation" || "$stopped_invocation" == "${original_invocations[$service]}" ]] ||
      die "$service stop belongs to a different invocation"
  done
  assert_coordinated_hold
  receipt="${release_paths[hololive-api]}/quiesced"
  hold_receipt >"$receipt.new"
  mv -T -- "$receipt.new" "$receipt"
  sync_paths "$receipt" "${release_paths[hololive-api]}"
  echo "[OUTCOME] quiesced api_release=${release_paths[hololive-api]} alarm_release=${release_paths[hololive-alarm-worker]}: migrate and activate only with explicit --resume after the coordinated peer cutover"
  exit 0
fi

stage=migration
run_migration() {
  local migrate_dropins="$quadlet_dir/hololive-db-migrate.container.d" migrate_tmp
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
  for service in "${services[@]}"; do assert_writer_endpoint "$service"; done
  if [[ "$mode" == resume ]]; then
    check_receipt
    assert_running_pooler_image
    mkdir -- "$migration"
    sync_paths "$migration" "${release_paths[hololive-api]}"
  fi
  systemctl start hololive-db-migrate.service
  assert_no_fence_marker
  if [[ "$mode" == resume ]]; then
    printf '%s\n' "$migrator_id" >"$migration/completed"
    sync_paths "$migration/completed" "$migration"
  fi
  echo "[MIGRATE] hololive-db-migrate completed with $migrator_id"
}
if [[ "$mode" == resume ]]; then
  migration="${release_paths[hololive-api]}/migration"
  if [[ -e "$migration" ]]; then
    cmp -s "$migration/completed" <(printf '%s\n' "$migrator_id") ||
      die "previous migration failed or its outcome is unknown; not retrying"
  else
    run_migration
  fi
else
  run_migration
fi
stage=writer-sql
assert_running_pooler_image
for service in "${services[@]}"; do
  assert_writer_endpoint "$service"
  postgres_pooler_assert_writer_sql "$service"
done
assert_running_pooler_image
assert_no_fence_marker

stage=activation
[[ "$mode" != resume ]] || check_receipt
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
for service in "${services[@]}"; do
  release="${release_paths[$service]}"
  stage=activation
  unit="$service.service"
  assert_writer_endpoint "$service"
  if [[ "$mode" == resume ]]; then
    assert_stopped "$service" || die "$unit: refusing to release a foreign or incomplete hold"
    opened+=("$service")
    systemctl unmask "$unit"
  fi
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
done
for service in "${services[@]}"; do
  echo "[OUTCOME] deployed service=$service revision=$revision image=${candidate_ids[$service]} release=${release_paths[$service]}"
done
