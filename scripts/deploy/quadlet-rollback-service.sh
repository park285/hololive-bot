#!/usr/bin/env bash
set -Eeuo pipefail

die() { echo "[quadlet-rollback] $*" >&2; exit 1; }
usage() { echo "Usage: $0 <release-dir> [--check|--apply]" >&2; exit 2; }

[[ $# -ge 1 && $# -le 2 ]] || usage
mode="${2:---check}"
[[ "$mode" == --check || "$mode" == --apply ]] || usage

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  [[ -z "${HOLOLIVE_QUADLET_TEST_ROOT:-}" ]] || die "HOLOLIVE_QUADLET_TEST_ROOT is only for non-root tests"
  prefix=""
else
  prefix="${HOLOLIVE_QUADLET_TEST_ROOT:-}"
  [[ -n "$prefix" ]] || die "must run as root (sudo)"
fi
deploy_root="$(realpath -m -- "$prefix/opt/hololive-bot/compose/current")"
fence_dir="$prefix/var/lib/hololive-postgres-fence"
if [[ -z "$prefix" && "$ROOT_DIR" != "$deploy_root" ]]; then
  die "run the deployed copy: $deploy_root/scripts/deploy/quadlet-rollback-service.sh"
fi
pair_tool="$ROOT_DIR/scripts/deploy/lib/worker-profile-pair.py"
lock_dir="$prefix/run/lock"
install -d -m 0755 "$lock_dir"
exec 9>"$lock_dir/hololive-quadlet-redeploy.lock"
flock -n 9 || die "another central Quadlet writer is running"

release="$(realpath -e -- "$1")" || die "release directory not found: $1"
[[ "$release" == "$deploy_root/artifacts/worker-profile/releases/"* ]] ||
  die "release must live under $deploy_root/artifacts/worker-profile/releases"
service="$(<"$release/service")"
case "$service" in
  hololive-api|hololive-alarm-worker) ;;
  *) die "unsupported service recorded in $release/service" ;;
esac
unit="$service.service"
previous="$release/previous"
installed_unit="$prefix/etc/containers/systemd/hololive-bot/$service.container"

python3 "$pair_tool" verify "$service" "$previous"
python3 "$pair_tool" check "$service" "$previous"
restore_unit=false
if [[ -f "$previous/unit.container" ]] && ! cmp -s -- "$previous/unit.container" "$installed_unit"; then
  restore_unit=true
elif [[ ! -f "$previous/unit.container" ]]; then
  echo "[WARN] $previous has no recorded base unit; the installed unit definition stays as is" >&2
fi
if [[ "$mode" == --check ]]; then
  [[ "$restore_unit" != true ]] || echo "[CHECK] --apply also restores the base unit the previous image ran with: $installed_unit"
  echo "[CHECK] $service previous pair verified: $previous"
  exit 0
fi

for marker in fence.intent fenced; do
  [[ ! -e "$fence_dir/$marker" ]] || die "primary fence marker present: $fence_dir/$marker"
done
if [[ "$restore_unit" == true ]]; then
  cp -p -- "$installed_unit" "$release/rollback-replaced-$service.container"
  unit_tmp="$(mktemp "$(dirname -- "$installed_unit")/.$service.container.XXXXXX")"
  install -m 0644 -- "$previous/unit.container" "$unit_tmp"
  mv -f -- "$unit_tmp" "$installed_unit"
  echo "[ROLLBACK] restored the base unit the previous image ran with; run sync-opt-current.sh for the intended revision before the next deployment"
fi
python3 "$pair_tool" activate "$service" "$previous" "$deploy_root"
systemctl daemon-reload
systemctl restart "$unit"
systemctl is-active --quiet "$unit" || die "$unit is not active after restart"
python3 "$pair_tool" verify-live "$service" "$previous" "$service"
restarts="$(systemctl show "$unit" -p NRestarts --value)"
[[ "$restarts" == 0 ]] || die "$unit restarted automatically after rollback (NRestarts=$restarts)"
echo "[ROLLBACK] $service restored from $previous"
