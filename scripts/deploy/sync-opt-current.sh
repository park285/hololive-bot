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
source_base=
valkey_only=false
case "$#:${1:-}" in
  0:) ;;
  1:--valkey-only) valkey_only=true ;;
  1:--preserve-runtime) preserve_runtime=true ;;
  1:--application-only) application_only=true; preserve_runtime=true ;;
  2:--source-only)
    [[ "$2" =~ ^[0-9a-f]{40}$ ]] || die "source-only requires the full installed commit"
    source_base="$2"; application_only=true; preserve_runtime=true ;;
  *) die "usage: $0 [--valkey-only | --preserve-runtime | --application-only | --source-only <installed-commit>]" ;;
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
env_value() {
  if compose_env_key_exists_in_file "$COMPOSE_ENV" "$1"; then
    compose_env_read_value_from_file "$COMPOSE_ENV" "$1"
  fi
}
require_central_valkey_bind() {
  compose_env_validate_file_format "$COMPOSE_ENV"
  [[ "$(env_value VALKEY_BIND_IP)" == 100.100.1.8 ]] || die "VALKEY_BIND_IP must select the central Valkey endpoint"
}
if [[ $valkey_only == true ]]; then
  # shellcheck source=scripts/deploy/lib/compose-env.sh
  . "$REPO_ROOT/scripts/deploy/lib/compose-env.sh"
  require_central_valkey_bind
  python3 -I /opt/iris-stack/valkey-ha/stage.py selected hololive || die "Valkey standalone stage is not admitted"
  unit=valkey-cache.container
  target="$QUADLET_DIR/$unit"
  [[ -f $target && ! -L $target && $(realpath -e "$target") == "$target" && ! -e $target.d && ! -L $target.d ]] ||
    die "expected an existing Valkey unit without overrides"
  [[ -x $LIBEXEC_DIR/quadlet-prestart ]] || die "installed Quadlet prestart missing"
  scratch=$(mktemp -d /run/.hololive-valkey-unit.XXXXXX)
  candidate=
  trap 'rm -rf -- "$scratch"; [[ -z $candidate ]] || rm -f -- "$candidate"' EXIT
  mkdir "$scratch/units"
  cp -a "$QUADLET_DIR/." "$scratch/units/"
  install -m0644 "$REPO_ROOT/deploy/quadlet/hololive-bot/$unit" "$scratch/units/$unit"
  QUADLET_UNIT_DIRS="$scratch/units" /usr/libexec/podman/quadlet -dryrun >/dev/null || die "Valkey unit rejected"
  candidate=$(mktemp "$QUADLET_DIR/.valkey-candidate.XXXXXX")
  install -m0644 "$scratch/units/$unit" "$candidate"
  mv -T -- "$candidate" "$target"
  candidate=
  systemctl daemon-reload || die "Valkey unit published; daemon-reload outcome unknown"
  log "Valkey unit published without a service restart"
  exit 0
fi
if [[ -n "$source_base" ]]; then
  [[ -z "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=all)" ]] || die "source-only requires a clean checkout"
  if ! { [[ -f "$OPT_CURRENT/.source-revision" && ! -L "$OPT_CURRENT/.source-revision" ]] &&
    cmp -s "$OPT_CURRENT/.source-revision" <(printf '%s\n' "$source_base"); }; then
    die "source-only installed revision mismatch"
  fi
  if ! { [[ "$source_base" != "$HEAD_REVISION" ]] && git -C "$REPO_ROOT" merge-base --is-ancestor "$source_base" "$HEAD_REVISION"; }; then
    die "source-only requires a descendant of the installed revision"
  fi
  reserved="$(git -C "$REPO_ROOT" ls-tree "$source_base" -- .source-revision)$(git -C "$REPO_ROOT" ls-tree "$HEAD_REVISION" -- .source-revision)"
  [[ -z "$reserved" ]] || die "reserved source-revision path in Git tree"
  [[ "$(git -C "$REPO_ROOT" hash-object --stdin <"${BASH_SOURCE[0]}")" == "$(git -C "$REPO_ROOT" rev-parse "$HEAD_REVISION:scripts/deploy/sync-opt-current.sh")" ]] ||
    die "source-only bootstrap must be the reviewed coordinator"
fi

if [[ "$preserve_runtime" == true ]]; then
  for d in data logs backups runtime-config; do
    [[ -d "$OPT_CURRENT/$d" ]] || die "$1 requires an existing runtime directory: $OPT_CURRENT/$d"
  done
fi

backup_current() {
  local destination
  local -a exclusions=(--exclude=/data --exclude=/logs --exclude=/backups --exclude=/artifacts/worker-profile)
  [[ -d "$OPT_CURRENT" ]] || return 0
  destination="$(mktemp -d "${OPT_CURRENT}.bak-${HEAD_SHA}-XXXXXX")"
  [[ "$preserve_runtime" != true ]] || exclusions+=(--exclude=/runtime-config)
  rsync -a "${exclusions[@]}" "$OPT_CURRENT/" "$destination/"
  backup="$destination"
  log "backup -> $backup (previous recovery points retained)"
}

if [[ "$application_only" != true ]]; then
  python3 -I /opt/iris-stack/valkey-ha/stage.py selected hololive || die "Valkey standalone stage is not admitted"
  backup_current
fi

mkdir -p "$OPT_ROOT"

STAGING="$(mktemp -d "${OPT_ROOT}/.staging-current.XXXXXX")"
QUADLET_CHECK="$(mktemp -d "${OPT_ROOT}/.quadlet-check.XXXXXX")"
cleanup() { rm -rf "$STAGING" "$QUADLET_CHECK"; }
trap cleanup EXIT
if [[ -n "$source_base" ]]; then
  changes="$QUADLET_CHECK/changes"
  git -C "$REPO_ROOT" diff --no-renames --no-ext-diff --raw --no-abbrev -z "$source_base" "$HEAD_REVISION" >"$changes"
  replacements=()
  while IFS= read -r -d '' header && IFS= read -r -d '' path; do
    read -r old_mode new_mode old_blob new_blob status <<<"${header#:}"
    [[ "$status" == M && "$old_mode" == "$new_mode" && "$old_mode" =~ ^100(644|755)$ ]] || die "source-only requires regular-file modifications"
    case "$path" in
      hololive/*/scripts/*|hololive/*/internal/migrationrunner/*|scripts/ci/postgres-capacity-policy.tsv) die "protected source path: $path" ;;
      README.md|CHANGELOG.md|VERSION|go.work|go.work.sum|.github/workflows/*.yml|docs/*|deploy/compose/README.md|hololive/*|scripts/README.md|scripts/ci/*|scripts/deploy/sync-opt-current.sh|scripts/deploy/sync-opt-current_test.sh) ;;
      *) die "unapproved source-only path: $path" ;;
    esac
    live="$OPT_CURRENT/$path"
    [[ -f "$live" && ! -L "$live" && "$(realpath -e "$live")" == "$live" && "$(stat -c %h "$live")" == 1 &&
      "$(git hash-object --no-filters "$live")" == "$old_blob" && "$(git hash-object --no-filters "$REPO_ROOT/$path")" == "$new_blob" ]] ||
      die "source-only content or path mismatch: $path"
    replacements+=("$path")
  done <"$changes"
  (( ${#replacements[@]} > 0 )) || die "source-only has no replacements"
  python3 -I -S - "$OPT_CURRENT" <<'PY'
import os
import stat
import sys
from pathlib import Path

root = Path(sys.argv[1])
programs = [
    "scripts/deploy/quadlet-redeploy-service.sh", "scripts/deploy/quadlet-rollback-service.sh",
    "scripts/deploy/quadlet-prestart.sh", "scripts/ci/postgres-capacity-policy.tsv",
    *("scripts/deploy/lib/" + name for name in (
        "worker-profile-pair.py", "systemd-stop-guard.py", "writer-drain-guard.py", "admin-bind.sh",
        "public-bind-mounts.sh", "postgres-capacity.sh", "compose-env.sh", "postgres-endpoint.sh")),
    "hololive/hololive-api/cmd/db-migrate/queries/bootstrap_writer_admission.sql",
]
slot_table = ("collector-slots.sh", "collector-slots.tsv")
files = {root / path for path in programs}
installed = Path("/usr/local/libexec/hololive-bot")
files.add(installed / "quadlet-prestart")
files.update(installed / "lib" / name for name in (
    "admin-bind.sh", "public-bind-mounts.sh", "postgres-capacity.sh", "compose-env.sh", "postgres-endpoint.sh"))
files.update(path for name in slot_table for path in (root / "scripts/deploy/lib" / name, installed / "lib" / name)
             if os.path.lexists(path))
directories = {parent for path in files for parent in path.parents}
for path in sorted(directories | files):
    info = path.lstat()
    kind_ok = stat.S_ISDIR(info.st_mode) if path in directories else stat.S_ISREG(info.st_mode) and info.st_nlink == 1
    if (not kind_ok or info.st_uid != 0 or info.st_mode & 0o022 or
            any(name.startswith("system.posix_acl_") for name in os.listxattr(path, follow_symlinks=False))):
        sys.exit(f"unsafe installed coordinator authority: {path}")
PY
fi
git -C "$REPO_ROOT" archive "$HEAD_REVISION" | tar -x -C "$STAGING"
printf '%s\n' "$HEAD_REVISION" >"$STAGING/.source-revision"
log "staged tracked tree ($(find "$STAGING" -type f | wc -l) files)"

if [[ "$preserve_runtime" == true ]]; then
  bash "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "$OPT_CURRENT" ||
    die "existing execution tree ownership is unsafe; nothing was installed"
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
for lib in admin-bind public-bind-mounts postgres-capacity compose-env postgres-endpoint collector-slots; do
  host_assets+=("scripts/deploy/lib/$lib.sh|$LIBEXEC_DIR/lib/$lib.sh|644")
done
host_assets+=("scripts/deploy/lib/collector-slots.tsv|$LIBEXEC_DIR/lib/collector-slots.tsv|644")

assert_same_file() {
  if ! { [[ -f "$1" && ! -L "$1" && -f "$2" && ! -L "$2" ]] && cmp -s -- "$1" "$2"; }; then
    die "application-only file drift: $2"
  fi
}

assert_same_tree() {
  local source="$1" dest="$2" changes
  shift 2
  [[ -d "$source" && ! -L "$source" && -d "$dest" && ! -L "$dest" ]] ||
    die "application-only tree drift: $dest"
  changes="$(rsync -rlDEcni --delete "$@" "$source/" "$dest/")"
  [[ -z "$changes" ]] || die "application-only tree drift: $dest ($changes)"
}

verify_execution_tree() {
  local verifier="$1" asset path dest unit
  shift
  local -a paths=("$@" "$OPT_CURRENT/scripts/deploy/verify-exec-tree-ownership.sh"
    "$OPT_CURRENT/scripts/ci/postgres-capacity-policy.tsv"
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

# shellcheck source=scripts/deploy/lib/compose-env.sh
. "$STAGING/scripts/deploy/lib/compose-env.sh"
# shellcheck source=scripts/deploy/lib/collector-slots.sh
. "$STAGING/scripts/deploy/lib/collector-slots.sh"
require_central_valkey_bind
central_collector="$(collector_slot "$(collector_slots_where host central)" collector_unit)" ||
  die "central collector slot is missing"
central_collector="${central_collector%.service}"
OPTIONAL_FEATURES=(
  "x-spaces|HOLOLIVE_X_SPACES_ENABLED|40-x-spaces.conf|hololive-api hololive-alarm-worker"
  "schedule-poster|HOLOLIVE_SCHEDULE_POSTER_ENABLED|41-schedule-poster.conf|hololive-api"
  "schedule-poster-review|HOLOLIVE_SCHEDULE_POSTER_REVIEW_ENABLED|42-schedule-poster-review.conf|hololive-api"
  "broadcast-type-decision|HOLOLIVE_BROADCAST_TYPE_DECISION_ENABLED|43-broadcast-type-decision.conf|hololive-api"
  "event-grade|HOLOLIVE_EVENT_GRADE_ENABLED|44-event-grade.conf|$central_collector"
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
if [[ "$application_only" == true ]]; then
  assert_same_tree "$quadlet_check_dir" "$QUADLET_DIR" "--exclude=/$migrator"
  required_exec=--configure-login-roles
  if [[ -n "$source_base" ]]; then
    required_exec=--schema-only
    assert_same_file "$STAGING/$migrator_path" "$QUADLET_DIR/$migrator"
  fi
  [[ "$(awk -F= '/^\[/{s=$0} s=="[Container]" && $1=="Exec"{print substr($0,6)}' "$QUADLET_DIR/$migrator")" == "$required_exec" ]] ||
    die "installed migrator must use $required_exec"
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

if [[ -n "$source_base" ]]; then
  previous="$QUADLET_CHECK/previous"
  mkdir "$previous"
  git -C "$REPO_ROOT" archive "$source_base" | tar -x -C "$previous"
  tree_filters=(--exclude=/.source-revision --exclude=/data --exclude=/logs --exclude=/backups --exclude=/runtime-config --exclude=/artifacts/worker-profile)
  if [[ -d "$OPT_CURRENT/artifacts/worker-profile" ]]; then
    mkdir -p "$previous/artifacts" "$STAGING/artifacts"
  fi
  assert_same_tree "$previous" "$OPT_CURRENT" "${tree_filters[@]}"
  replacements+=(.source-revision)
  prepared="$QUADLET_CHECK/replacements"
  mkdir "$prepared"
  verify_paths=()
  for path in "${replacements[@]}"; do
    verify_paths+=("$OPT_CURRENT/$path")
    mkdir -p "$prepared/$(dirname "$path")"
    cp --preserve=all "$OPT_CURRENT/$path" "$prepared/$path"
    cat "$STAGING/$path" >"$prepared/$path"
  done
  bash "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "${verify_paths[@]}"
  backup_current
  cmp -s "$backup/.source-revision" "$OPT_CURRENT/.source-revision" || die "source-only backup stamp mismatch"
  assert_same_tree "$previous" "$backup" "${tree_filters[@]}"
  cp --preserve=all "$OPT_CURRENT/.source-revision" "$QUADLET_CHECK/incomplete"
  printf 'incomplete %s -> %s\n' "$source_base" "$HEAD_REVISION" >"$QUADLET_CHECK/incomplete"
  sync -f "$OPT_ROOT"
  mv -T --no-copy "$QUADLET_CHECK/incomplete" "$OPT_CURRENT/.source-revision"
  sync -f "$OPT_ROOT"
  for path in "${replacements[@]}"; do
    if [[ "$path" == .source-revision ]]; then
      assert_same_tree "$STAGING" "$OPT_CURRENT" "${tree_filters[@]}"
      sync -f "$OPT_CURRENT"
    fi
    mv -T --no-copy "$prepared/$path" "$OPT_CURRENT/$path"
  done
  sync -f "$OPT_CURRENT"
  log "source-only published $source_base -> $HEAD_REVISION; host definitions and runtime untouched"
  exit 0
fi

if [[ "$application_only" == true ]]; then
  backup_current
fi

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
mkdir -p /etc/nftables.d
for asset in "${host_assets[@]}"; do
  IFS='|' read -r path dest mode <<<"$asset"
  install -m"$mode" -o root -g root "$STAGING/$path" "$dest"
done
log "prestart and admin-dashboard ingress firewall installed"

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
