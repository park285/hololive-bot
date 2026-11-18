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

install -d -m 0755 /run/lock
exec 9>/run/lock/hololive-quadlet-redeploy.lock
flock -n 9 || die "another central Quadlet writer is running"

if [[ "$(hostname -s)" == kapu && "${HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED:-}" != 1 ]]; then
  die "refusing to install central Quadlet units on build-control host kapu; set HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1 only for an approved rollback"
fi

assert_clean() {
  local dir="$1" name="$2"
  git -C "$dir" rev-parse --git-dir >/dev/null 2>&1 || die "$name git tree missing: $dir"
  if [[ -n "$(git -C "$dir" status --porcelain --untracked-files=no)" ]]; then
    git -C "$dir" status --short --untracked-files=no >&2
    die "$name has uncommitted tracked changes; commit/stash before sync"
  fi
}

assert_clean "$REPO_ROOT" "repo"
HEAD_REVISION="$(git -C "$REPO_ROOT" rev-parse --verify 'HEAD^{commit}')"
HEAD_SHA="$(git -C "$REPO_ROOT" rev-parse --short "$HEAD_REVISION")"
log "repo=$REPO_ROOT HEAD=$HEAD_SHA -> $OPT_CURRENT"

if [[ "$preserve_runtime" == true ]]; then
  for d in data logs backups runtime-config; do
    [[ -d "$OPT_CURRENT/$d" ]] || die "--preserve-runtime requires an existing runtime directory: $OPT_CURRENT/$d"
  done
fi

if [[ -d "$OPT_CURRENT" ]]; then
  BACKUP="$(mktemp -d "${OPT_CURRENT}.bak-${HEAD_SHA}-XXXXXX")"
  backup_exclusions=(--exclude=/data --exclude=/logs --exclude=/backups --exclude=/artifacts/worker-profile)
  [[ "$preserve_runtime" != true ]] || backup_exclusions+=(--exclude=/runtime-config)
  rsync -a "${backup_exclusions[@]}" "$OPT_CURRENT/" "$BACKUP/"
  log "backup -> $BACKUP (previous recovery points retained)"
fi

mkdir -p "$OPT_ROOT"

STAGING="$(mktemp -d "${OPT_ROOT}/.staging-current.XXXXXX")"
QUADLET_CHECK="$(mktemp -d "${OPT_ROOT}/.quadlet-check.XXXXXX")"
cleanup() { rm -rf "$STAGING" "$QUADLET_CHECK"; }
trap cleanup EXIT
git -C "$REPO_ROOT" archive "$HEAD_REVISION" | tar -x -C "$STAGING"
printf '%s\n' "$HEAD_REVISION" >"$STAGING/.source-revision"
log "staged tracked tree ($(find "$STAGING" -type f | wc -l) files)"

if [[ "$preserve_runtime" == true ]]; then
  bash "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "$OPT_CURRENT" ||
    die "existing execution tree ownership is unsafe; nothing was installed"
  chmod --reference="$OPT_CURRENT" "$STAGING"
  chown --reference="$OPT_CURRENT" "$STAGING"
fi

# shellcheck source=scripts/deploy/lib/compose-env.sh
. "$STAGING/scripts/deploy/lib/compose-env.sh"
compose_env_validate_file_format "$COMPOSE_ENV"
env_value() {
  if compose_env_key_exists_in_file "$COMPOSE_ENV" "$1"; then
    compose_env_read_value_from_file "$COMPOSE_ENV" "$1"
  fi
}
[[ "$(env_value HOLOLIVE_VALKEY_HA_ENABLED)" == 1 ]] || die "HOLOLIVE_VALKEY_HA_ENABLED=1 is required in $COMPOSE_ENV"
[[ -n "$(env_value VALKEY_SENTINEL_ADDRS)" ]] || die "VALKEY_SENTINEL_ADDRS is required in $COMPOSE_ENV"
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
# shellcheck source=scripts/deploy/lib/postgres-capacity.sh
. "$STAGING/scripts/deploy/lib/postgres-capacity.sh"
postgres_capacity_assert_target "$STAGING" "$COMPOSE_ENV" || die "PostgreSQL connection budget check failed for $COMPOSE_ENV; nothing was installed"

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

mkdir -p "$OPT_CURRENT"
rsync -a --delete \
  --exclude=/data --exclude=/logs --exclude=/backups --exclude=/runtime-config --exclude=/artifacts/worker-profile \
  "$STAGING/" "$OPT_CURRENT/"

if [[ "$preserve_runtime" == true ]]; then
  log "synced execution tree (runtime bytes and ownership unchanged)"
else
  for d in data runtime-config; do
    if [[ -d "$REPO_ROOT/$d" ]]; then
      mkdir -p "$OPT_CURRENT/$d"
      rsync -a --ignore-existing "$REPO_ROOT/$d/" "$OPT_CURRENT/$d/"
    fi
  done
  mkdir -p "$OPT_CURRENT/logs" "$OPT_CURRENT/backups"
  log "synced current (runtime data + runtime-config preserved)"

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

install -d -m0755 -o root -g root "$LIBEXEC_DIR" "$LIBEXEC_DIR/lib"
install -m0644 -o root -g root "$STAGING/scripts/deploy/lib/compose-env.sh" "$LIBEXEC_DIR/lib/compose-env.sh"
install -m0755 -o root -g root "$STAGING/scripts/deploy/quadlet-prestart.sh" "$LIBEXEC_DIR/quadlet-prestart"
install -m0644 -o root -g root "$STAGING/scripts/deploy/lib/admin-bind.sh" "$LIBEXEC_DIR/lib/admin-bind.sh"
install -m0644 -o root -g root "$STAGING/scripts/deploy/lib/public-bind-mounts.sh" "$LIBEXEC_DIR/lib/public-bind-mounts.sh"
log "prestart installed -> $LIBEXEC_DIR"

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
  "$LIBEXEC_DIR/lib/compose-env.sh"
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
