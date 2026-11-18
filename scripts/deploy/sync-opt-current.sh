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
application_only=false
case "$#:${1:-}" in
  0:) ;;
  1:--preserve-runtime) preserve_runtime=true ;;
  1:--application-only) application_only=true; preserve_runtime=true ;;
  *) die "usage: $0 [--preserve-runtime | --application-only]" ;;
esac

[[ "$(id -u)" -eq 0 ]] || die "must run as root (sudo)"

install -d -m 0755 /run/lock
exec 9>/run/lock/hololive-quadlet-redeploy.lock
flock -n 9 || die "another central Quadlet writer is running"

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
    [[ -d "$OPT_CURRENT/$d" ]] || die "$1 requires an existing runtime directory: $OPT_CURRENT/$d"
  done
fi

backup_current() {
  local backup
  local -a exclusions=(--exclude=/data --exclude=/logs --exclude=/backups --exclude=/artifacts/worker-profile)
  [[ -d "$OPT_CURRENT" ]] || return 0
  # 기존 복구점은 덮거나 정리하지 않는다. 보존 정리는 별도 승인된 운영 절차가 소유한다.
  backup="$(mktemp -d "${OPT_CURRENT}.bak-${HEAD_SHA}-XXXXXX")"
  [[ "$preserve_runtime" != true ]] || exclusions+=(--exclude=/runtime-config)
  rsync -a "${exclusions[@]}" "$OPT_CURRENT/" "$backup/"
  log "backup -> $backup (previous recovery points retained)"
}

if [[ "$application_only" != true ]]; then
  backup_current
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

migrator=hololive-db-migrate.container
migrator_path="deploy/quadlet/hololive-bot/$migrator"
host_assets=(
  "scripts/deploy/quadlet-prestart.sh|$LIBEXEC_DIR/quadlet-prestart|755"
  "scripts/systemd/admin-dashboard-ingress-firewall.service|/etc/systemd/system/admin-dashboard-ingress-firewall.service|644"
  "scripts/systemd/admin-dashboard-ingress.nft|/etc/nftables.d/admin-dashboard-ingress.nft|644"
)
for lib in admin-bind public-bind-mounts postgres-capacity compose-env postgres-endpoint; do
  host_assets+=("scripts/deploy/lib/$lib.sh|$LIBEXEC_DIR/lib/$lib.sh|644")
done

assert_same_file() {
  [[ -f "$1" && ! -L "$1" && -f "$2" && ! -L "$2" ]] && cmp -s -- "$1" "$2" ||
    die "application-only file drift: $2"
}

assert_same_tree() {
  local source="$1" dest="$2" changes
  shift 2
  [[ -d "$source" && ! -L "$source" && -d "$dest" && ! -L "$dest" ]] ||
    die "application-only tree drift: $dest"
  # archive 시각이 아니라 파일 종류·실행 비트·내용 및 추가/삭제를 비교한다.
  changes="$(rsync -rlDEcni --delete "$@" "$source/" "$dest/")"
  [[ -z "$changes" ]] || die "application-only tree drift: $dest ($changes)"
}

verify_execution_tree() {
  local verifier="$1" asset path dest unit
  shift
  local -a paths=("$@" "$OPT_CURRENT/scripts/deploy/verify-exec-tree-ownership.sh"
    "$OPT_CURRENT/scripts/ci/postgres-capacity-policy.tsv"
    "$OPT_CURRENT/deploy/pgbouncer/pg_hba.conf"
    "$OPT_CURRENT/deploy/pgbouncer/stats-users.txt"
    "$OPT_CURRENT/deploy/nginx/admin-dashboard-ingress.conf.template"
    "$OPT_CURRENT/deploy/postgres/pg_hba.conf")
  for asset in "${host_assets[@]}"; do
    IFS='|' read -r path dest _ <<<"$asset"
    paths+=("$dest" "$OPT_CURRENT/$path")
  done
  for unit in "$QUADLET_DIR"/* "$QUADLET_DIR"/*.d/*.conf; do
    [[ ! -e "$unit" ]] || paths+=("$unit")
  done
  bash "$verifier" "${paths[@]}" || die "exec-tree ownership verification failed"
}

sync_flags=()
if [[ "$application_only" == true ]]; then
  init_db=hololive/hololive-api/scripts/init-db
  protected=(deploy "$init_db" scripts/systemd scripts/ci/postgres-capacity-policy.tsv)
  deploy_exceptions=("$migrator_path")
  for path in deploy/{quadlet,compose,podman/{iris-seoul,kapu}}/README.md; do
    [[ ! -f "$STAGING/$path" || ! -f "$OPT_CURRENT/$path" ]] || deploy_exceptions+=("$path")
  done
  deploy_filters=()
  for path in "${deploy_exceptions[@]}"; do
    deploy_filters+=("--exclude=/${path#deploy/}")
    [[ -f "$STAGING/$path" && ! -L "$STAGING/$path" && -f "$OPT_CURRENT/$path" && ! -L "$OPT_CURRENT/$path" ]] ||
      die "application-only requires a regular deployment file: $path"
  done
  assert_same_tree "$STAGING/deploy" "$OPT_CURRENT/deploy" "${deploy_filters[@]}"
  assert_same_tree "$STAGING/$init_db" "$OPT_CURRENT/$init_db"
  assert_same_tree "$STAGING/scripts/systemd" "$OPT_CURRENT/scripts/systemd"
  assert_same_file "$STAGING/scripts/ci/postgres-capacity-policy.tsv" "$OPT_CURRENT/scripts/ci/postgres-capacity-policy.tsv"

  verify_paths=("$OPT_CURRENT/.source-revision" "$OPT_CURRENT/$init_db"/*)
  for asset in "${host_assets[@]}"; do
    IFS='|' read -r path dest mode <<<"$asset"
    assert_same_file "$STAGING/$path" "$OPT_CURRENT/$path"
    assert_same_file "$STAGING/$path" "$dest"
    [[ "$(stat -c '%u:%g:%a' "$dest")" == "0:0:$mode" ]] || die "application-only installation mode drift: $dest"
    protected+=("$path")
  done
  assert_same_file "$OPT_CURRENT/$migrator_path" "$QUADLET_DIR/$migrator"
  [[ -f "$OPT_CURRENT/.source-revision" && ! -L "$OPT_CURRENT/.source-revision" ]] ||
    die "application-only requires a deployed source revision"
  # shellcheck source=scripts/deploy/lib/source-revision.sh
  . "$STAGING/scripts/deploy/lib/source-revision.sh"
  deploy_assert_full_revision "$(<"$OPT_CURRENT/.source-revision")"

  # 설치된 심볼릭 링크를 임시 drop-in 트리로 복사하면 삭제/생성이 원본까지 따라갈 수 있다.
  links="$(find "$QUADLET_DIR" -type l -print -quit)"
  [[ -z "$links" ]] || die "application-only symlink in Quadlet installation: $links"
  for path in "${protected[@]}"; do
    sync_flags+=("--exclude=/$path")
    verify_paths+=("$OPT_CURRENT/$path")
  done
  for path in "${deploy_exceptions[@]}"; do
    verify_paths+=("$OPT_CURRENT/$path")
  done
  verify_execution_tree "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "${verify_paths[@]}"
  sync_flags+=(--checksum)
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
  "broadcast-type-decision|HOLOLIVE_BROADCAST_TYPE_DECISION_ENABLED|43-broadcast-type-decision.conf|hololive-api"
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
if [[ "$application_only" == true ]]; then
  assert_same_tree "$quadlet_check_dir" "$QUADLET_DIR" "--exclude=/$migrator"
  # 기존 image 고정 drop-in을 포함한 실제 생성 결과로 실행 인자와 자격 렌더링 경로를 확인한다.
  # 호스트 systemd에 반영하는 일은 이후 후보 image를 고정하는 배포기가 소유한다.
  [[ "$(awk -F= '/^\[/{s=$0} s=="[Container]" && $1=="Exec"{print substr($0,6)}' "$QUADLET_DIR/$migrator")" == --pool-only=auth ]] ||
    die "application-only requires the accepted pool-only migrator"
  generated="$QUADLET_CHECK/generated"
  mkdir "$generated"
  QUADLET_UNIT_DIRS="$quadlet_check_dir" /usr/libexec/podman/quadlet "$generated" ||
    die "staged Quadlet units do not generate; nothing was installed"
  awk '
    /^ExecStart=/ { starts++; if ($(NF-1) !~ /^(\$\{HOLOLIVE_API_IMAGE\}|sha256:[[:xdigit:]]+)$/ || $NF != "--schema-only") bad=1 }
    /^ExecStartPre=/ { if ($0=="ExecStartPre=/usr/local/libexec/hololive-bot/quadlet-prestart hololive-db-migrate schema") schema++; else bad=1 }
    END { exit !(starts==1 && schema==1 && !bad) }
  ' "$generated/${migrator%.container}.service" || die "application-only requires the schema-only migrator"
else
  QUADLET_UNIT_DIRS="$quadlet_check_dir" /usr/libexec/podman/quadlet -dryrun >/dev/null \
    || die "staged Quadlet units do not generate; nothing was installed"
fi
log "staged Quadlet units generate ($feature_summary)"

if [[ "$application_only" == true ]]; then
  backup_current
fi

# current 반영: tracked 트리만 교체하고 런타임 bind-mount 데이터는 보존한다. staging 에는
# data/logs/backups 가 없으므로 --delete 가 OPT_CURRENT 의 기존 런타임 데이터를 지우지 않도록
# 명시적으로 제외한다.
mkdir -p "$OPT_CURRENT"
rsync -a --delete "${sync_flags[@]}" \
  --exclude=/data --exclude=/logs --exclude=/backups --exclude=/runtime-config --exclude=/artifacts/worker-profile \
  "$STAGING/" "$OPT_CURRENT/"

if [[ "$application_only" == true ]]; then
  for path in "${deploy_exceptions[@]}"; do
    rsync -ac "$STAGING/$path" "$OPT_CURRENT/$path"
  done
  install -m0644 -o root -g root "$STAGING/$migrator_path" "$QUADLET_DIR/$migrator"
  verify_execution_tree "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "${verify_paths[@]}"
  log "application-only source and schema migrator published (HEAD=$HEAD_SHA); no daemon-reload or service activation"
  exit 0
fi

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
mkdir -p /etc/nftables.d
for asset in "${host_assets[@]}"; do
  IFS='|' read -r path dest mode <<<"$asset"
  install -m"$mode" -o root -g root "$STAGING/$path" "$dest"
done
log "prestart and admin-dashboard ingress firewall installed"

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

systemctl daemon-reload
log "daemon-reload done"

verify_execution_tree "$OPT_CURRENT/scripts/deploy/verify-exec-tree-ownership.sh"
log "exec-tree ownership verified"

log "SSOT sync complete (HEAD=$HEAD_SHA)"
log "next: api/alarm-worker release -> sudo $OPT_CURRENT/scripts/deploy/quadlet-redeploy-service.sh <service> <image> [<migrator-image>]; collector c -> po-central-cutover.sh c (from kapu); other unit changes apply on an approved restart"
