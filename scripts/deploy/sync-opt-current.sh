#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
OPT_ROOT="${OPT_ROOT:-/opt/hololive-bot/compose}"
OPT_CURRENT="${OPT_ROOT}/current"
QUADLET_DIR=/etc/containers/systemd/hololive-bot
LIBEXEC_DIR=/usr/local/libexec/hololive-bot
COMPOSE_ENV=/etc/stack-secrets/hololive-bot/compose.env

log() { printf '[sync-opt-current] %s\n' "$*"; }
die() { printf '[sync-opt-current] ERROR: %s\n' "$*" >&2; exit 1; }

preserve_runtime=false
case "$#:${1:-}" in
  0:) ;;
  1:--preserve-runtime) preserve_runtime=true ;;
  *) die "usage: $0 [--preserve-runtime]" ;;
esac

[[ "$(id -u)" -eq 0 ]] || die "must run as root (sudo)"

# 알림 dispatcher는 중앙 하나뿐이다. build-control host kapu에 중앙 unit을 설치하면 부팅 때
# 두 번째 alarm-worker가 뜨므로 승인된 rollback 표시 없이는 거부한다.
if [[ "$(hostname -s)" == kapu && "${HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED:-}" != 1 ]]; then
  die "refusing to install central Quadlet units on build-control host kapu; set HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1 only for an approved rollback"
fi

assert_clean() {
  local dir="$1" name="$2"
  git -C "$dir" rev-parse --git-dir >/dev/null 2>&1 || die "$name git tree missing: $dir"
  # HEAD 만 배포(git archive)하므로 tracked 변경만 SSOT 위반으로 본다. 서브모듈의 AGENTS.md/
  # CLAUDE.md 같은 의도된 untracked 로컬 파일은 clean 판정에서 제외한다.
  if [[ -n "$(git -C "$dir" status --porcelain --untracked-files=no)" ]]; then
    git -C "$dir" status --short --untracked-files=no >&2
    die "$name has uncommitted tracked changes; commit/stash before sync"
  fi
}

assert_clean "$REPO_ROOT" "repo"
# 배포 트리와 그 revision 기록이 같은 commit을 가리키도록 HEAD를 한 번만 해석해 archive에도 쓴다.
HEAD_REVISION="$(git -C "$REPO_ROOT" rev-parse --verify 'HEAD^{commit}')"
HEAD_SHA="$(git -C "$REPO_ROOT" rev-parse --short "$HEAD_REVISION")"
log "repo=$REPO_ROOT HEAD=$HEAD_SHA -> $OPT_CURRENT"

if [[ "$preserve_runtime" == true ]]; then
  for d in data logs backups runtime-config; do
    [[ -d "$OPT_CURRENT/$d" ]] || die "--preserve-runtime requires an existing runtime directory: $OPT_CURRENT/$d"
  done
fi

if [[ -d "$OPT_CURRENT" ]]; then
  # 기존 복구점은 덮거나 정리하지 않는다. 보존 정리는 별도 승인된 운영 절차가 소유한다.
  BACKUP="$(mktemp -d "${OPT_CURRENT}.bak-${HEAD_SHA}-XXXXXX")"
  backup_exclusions=(--exclude=/data --exclude=/logs --exclude=/backups --exclude=/artifacts/worker-profile)
  [[ "$preserve_runtime" != true ]] || backup_exclusions+=(--exclude=/runtime-config)
  rsync -a "${backup_exclusions[@]}" "$OPT_CURRENT/" "$BACKUP/"
  log "backup -> $BACKUP (previous recovery points retained)"
fi

mkdir -p "$OPT_ROOT"

# staging 은 OPT_ROOT 와 같은 파일시스템에 둔다: cross-device rsync(/tmp 별도 mount) 회피.
# QUADLET_CHECK 는 STAGING 밖에 둔다. STAGING 은 그대로 OPT_CURRENT 로 rsync 된다.
STAGING="$(mktemp -d "${OPT_ROOT}/.staging-current.XXXXXX")"
QUADLET_CHECK="$(mktemp -d "${OPT_ROOT}/.quadlet-check.XXXXXX")"
cleanup() { rm -rf "$STAGING" "$QUADLET_CHECK"; }
trap cleanup EXIT
git -C "$REPO_ROOT" archive "$HEAD_REVISION" | tar -x -C "$STAGING"
# quadlet-redeploy-service.sh가 배포 트리의 revision과 image revision label을 대조하는 기준이다.
printf '%s\n' "$HEAD_REVISION" >"$STAGING/.source-revision"
log "staged tracked tree ($(find "$STAGING" -type f | wc -l) files)"

if [[ "$preserve_runtime" == true ]]; then
  bash "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "$OPT_CURRENT" ||
    die "existing execution tree ownership is unsafe; nothing was installed"
  # rsync가 보호 디렉터리의 부모 mode·group까지 바꾸지 않도록 새 staging root만 맞춘다.
  chmod --reference="$OPT_CURRENT" "$STAGING"
  chown --reference="$OPT_CURRENT" "$STAGING"
fi

# 설치 대상 값은 /opt 반영 전에 확인한다. 실패하면 현재 트리와 unit을 그대로 둔다.
# shellcheck source=scripts/deploy/lib/compose-env.sh
. "$STAGING/scripts/deploy/lib/compose-env.sh"
compose_env_validate_file_format "$COMPOSE_ENV"
env_value() {
  if compose_env_key_exists_in_file "$COMPOSE_ENV" "$1"; then
    compose_env_read_value_from_file "$COMPOSE_ENV" "$1"
  fi
}
# 중앙 unit은 Sentinel discovery를 쓰는 HA 구성만 정의한다. 다른 구성으로 조용히 기동하지 않는다.
[[ "$(env_value HOLOLIVE_VALKEY_HA_ENABLED)" == 1 ]] || die "HOLOLIVE_VALKEY_HA_ENABLED=1 is required in $COMPOSE_ENV"
[[ -n "$(env_value VALKEY_SENTINEL_ADDRS)" ]] || die "VALKEY_SENTINEL_ADDRS is required in $COMPOSE_ENV"
# 선택 기능 drop-in: 이름|compose.env 플래그(0 또는 1)|drop-in 파일|적용 unit 목록.
OPTIONAL_FEATURES=(
  "x-spaces|HOLOLIVE_X_SPACES_ENABLED|40-x-spaces.conf|hololive-api hololive-alarm-worker"
  "schedule-poster|HOLOLIVE_SCHEDULE_POSTER_ENABLED|41-schedule-poster.conf|hololive-api"
  "schedule-poster-review|HOLOLIVE_SCHEDULE_POSTER_REVIEW_ENABLED|42-schedule-poster-review.conf|hololive-api"
)
declare -A feature_enabled=()
feature_summary=""
for feature in "${OPTIONAL_FEATURES[@]}"; do
  IFS='|' read -r name flag _ _ <<<"$feature"
  value="$(env_value "$flag")"
  case "${value:-0}" in
    0|1) ;;
    *) die "$flag must be 0 or 1" ;;
  esac
  feature_enabled[$name]="${value:-0}"
  feature_summary+="${feature_summary:+ }${name}=${value:-0}"
done
# 연결 예산 검사다. 호스트 compose.env의 pool 값이 max_connections 여유를
# 넘으면 unit을 설치하지 않는다. 출력은 key 이름과 합계만 담는다.
# shellcheck source=scripts/deploy/lib/postgres-capacity.sh
. "$STAGING/scripts/deploy/lib/postgres-capacity.sh"
postgres_capacity_assert_target "$STAGING" "$COMPOSE_ENV" || die "PostgreSQL connection budget check failed for $COMPOSE_ENV; nothing was installed"

# 설치 뒤 $QUADLET_DIR 와 같은 구성(staged unit, 선택 기능 drop-in, 설치기가 건드리지 않는 기존 drop-in)을
# 먼저 만들어 Quadlet 생성을 확인한다. 실패하면 /opt·prestart·unit 어느 것도 바꾸지 않는다.
quadlet_check_dir="$QUADLET_CHECK/hololive-bot"
mkdir -p "$quadlet_check_dir"
for dropin_dir in "$QUADLET_DIR"/*.d; do
  [[ -d "$dropin_dir" ]] || continue
  cp -a -- "$dropin_dir" "$quadlet_check_dir/"
done
cp -- "$STAGING"/deploy/quadlet/hololive-bot/* "$quadlet_check_dir/"
for feature in "${OPTIONAL_FEATURES[@]}"; do
  IFS='|' read -r name _ file units <<<"$feature"
  rm -f -- "$quadlet_check_dir"/*.container.d/"$file"
  [[ "${feature_enabled[$name]}" == 1 ]] || continue
  for unit in $units; do
    mkdir -p "$quadlet_check_dir/$unit.container.d"
    cp -- "$STAGING/deploy/quadlet/optional/$name/$unit.container.d/$file" "$quadlet_check_dir/$unit.container.d/"
  done
done
QUADLET_UNIT_DIRS="$quadlet_check_dir" /usr/libexec/podman/quadlet -dryrun >/dev/null \
  || die "staged Quadlet units do not generate; nothing was installed"
log "staged Quadlet units generate ($feature_summary)"

# current 반영: tracked 트리만 교체하고 런타임 bind-mount 데이터는 보존한다. staging 에는
# data/logs/backups 가 없으므로 --delete 가 OPT_CURRENT 의 기존 런타임 데이터를 지우지 않도록
# 명시적으로 제외한다.
mkdir -p "$OPT_CURRENT"
rsync -a --delete \
  --exclude=/data --exclude=/logs --exclude=/backups --exclude=/runtime-config --exclude=/artifacts/worker-profile \
  "$STAGING/" "$OPT_CURRENT/"

if [[ "$preserve_runtime" == true ]]; then
  # runtime-config·data의 시드와 기존 데이터 권한 변경은 artifact-only 갱신의 범위 밖이다.
  log "synced execution tree (runtime bytes and ownership unchanged)"
else
  # 전체 동기화는 승인된 작업 트리에서 아직 없는 runtime 입력만 시드한다.
  for d in data runtime-config; do
    if [[ -d "$REPO_ROOT/$d" ]]; then
      mkdir -p "$OPT_CURRENT/$d"
      rsync -a --ignore-existing "$REPO_ROOT/$d/" "$OPT_CURRENT/$d/"
    fi
  done
  mkdir -p "$OPT_CURRENT/logs" "$OPT_CURRENT/backups"
  log "synced current (runtime data + runtime-config preserved)"

  # 전체 동기화는 root 실행 트리와 컨테이너 uid 1000의 데이터 권한을 준비한다.
  chown -R root:root "$OPT_CURRENT"
  chmod -R go-w "$OPT_CURRENT"
  if [[ -d "$OPT_CURRENT/runtime-config" ]]; then
    chmod 0755 "$OPT_CURRENT/runtime-config"
  fi
  for d in data logs backups; do
    [[ -d "$OPT_CURRENT/$d" ]] && chown -R 1000:1000 "$OPT_CURRENT/$d"
  done
  chown root:root "$OPT_ROOT" "$(dirname "$OPT_ROOT")"
  log "ownership normalized (exec tree root, runtime data uid 1000)"
fi

# root 로 설치되는 systemd 자료(Quadlet unit·prestart)는 $REPO_ROOT(작업 트리)가 아니라 $STAGING(git
# archive HEAD 스냅샷)에서만 가져온다. 작업 트리에 심어진 untracked unit/drop-in이 root systemd에
# 주입(LPE)되지 않게 하고 assert_clean 이후 TOCTOU도 함께 막는다.
install -d -m0755 -o root -g root "$LIBEXEC_DIR" "$LIBEXEC_DIR/lib"
install -m0755 -o root -g root "$STAGING/scripts/deploy/quadlet-prestart.sh" "$LIBEXEC_DIR/quadlet-prestart"
install -m0644 -o root -g root "$STAGING/scripts/deploy/lib/admin-bind.sh" "$LIBEXEC_DIR/lib/admin-bind.sh"
install -m0644 -o root -g root "$STAGING/scripts/deploy/lib/public-bind-mounts.sh" "$LIBEXEC_DIR/lib/public-bind-mounts.sh"
log "prestart installed -> $LIBEXEC_DIR"

# release별 worker-profile drop-in(50-worker-profile.conf)은 worker-profile-pair.py가 소유하므로 건드리지 않는다.
install -d -m0755 -o root -g root "$QUADLET_DIR"
for unit in "$QUADLET_DIR"/*.container "$QUADLET_DIR"/*.volume "$QUADLET_DIR"/*.network; do
  [[ -e "$unit" ]] || continue
  [[ -e "$STAGING/deploy/quadlet/hololive-bot/${unit##*/}" ]] || rm -f -- "$unit"
done
install -m0644 -o root -g root "$STAGING"/deploy/quadlet/hololive-bot/* "$QUADLET_DIR/"
for feature in "${OPTIONAL_FEATURES[@]}"; do
  IFS='|' read -r name _ file units <<<"$feature"
  for unit in $units; do
    dropin="$QUADLET_DIR/$unit.container.d/$file"
    if [[ "${feature_enabled[$name]}" == 1 ]]; then
      install -d -m0755 -o root -g root "${dropin%/*}"
      install -m0644 -o root -g root "$STAGING/deploy/quadlet/optional/$name/$unit.container.d/$file" "$dropin"
    else
      rm -f -- "$dropin"
    fi
  done
done
log "Quadlet units installed -> $QUADLET_DIR ($feature_summary)"

install -m0644 -o root -g root \
  "$STAGING/scripts/systemd/admin-dashboard-ingress-firewall.service" \
  /etc/systemd/system/admin-dashboard-ingress-firewall.service
mkdir -p /etc/nftables.d
install -m0644 -o root -g root \
  "$STAGING/scripts/systemd/admin-dashboard-ingress.nft" \
  /etc/nftables.d/admin-dashboard-ingress.nft
log "admin-dashboard ingress firewall installed"

systemctl daemon-reload
log "daemon-reload done"

verifier="$OPT_CURRENT/scripts/deploy/verify-exec-tree-ownership.sh"
exec_tree=(
  "$verifier"
  "$LIBEXEC_DIR/quadlet-prestart"
  "$LIBEXEC_DIR/lib/admin-bind.sh"
  "$LIBEXEC_DIR/lib/public-bind-mounts.sh"
  "$OPT_CURRENT/deploy/nginx/admin-dashboard-ingress.conf.template"
  "$OPT_CURRENT/deploy/postgres/pg_hba.conf"
)
for unit in "$QUADLET_DIR"/* "$QUADLET_DIR"/*.container.d/*.conf; do
  [[ -f "$unit" ]] && exec_tree+=("$unit")
done
bash "$verifier" "${exec_tree[@]}" || die "exec-tree ownership verification failed"
log "exec-tree ownership verified"

log "SSOT sync complete (HEAD=$HEAD_SHA)"
log "next: api/alarm-worker release -> sudo $OPT_CURRENT/scripts/deploy/quadlet-redeploy-service.sh <service> <image> [<migrator-image>]; collector c -> po-central-cutover.sh c (from kapu); other unit changes apply on an approved restart"
