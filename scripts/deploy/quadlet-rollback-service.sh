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

. "$ROOT_DIR/scripts/deploy/lib/compose-env.sh"
. "$ROOT_DIR/scripts/deploy/lib/postgres-endpoint.sh"
. "$ROOT_DIR/scripts/deploy/lib/postgres-capacity.sh"

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
if [[ "$mode" == --apply ]]; then
  marker="$deploy_root/artifacts/worker-profile/releases/.central-writer-hold"
  [[ ! -e "$marker" && ! -L "$marker" ]] || die "ongoing coordinated writer hold"
  for peer in hololive-alarm-worker hololive-api; do
    mask="$prefix/etc/systemd/system/$peer.service"
    drop="$mask.d/90-hololive-stop-guard.conf"
    [[ ! -L "$mask" && ! -e "$drop" && ! -L "$drop" ]] || die "refusing to change a held writer"
  done
fi

python3 "$pair_tool" verify "$service" "$previous"
previous_image="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["image"])' "$previous/pair.json")"
[[ "$previous_image" =~ ^sha256:[0-9a-f]{64}$ ]] || die "invalid retained image ID"
compatible="$(podman image inspect --format '{{eq (index .Config.Labels "io.hololive-bot.outbox-schema") "text-file-v1"}}' "$previous_image")" ||
  die "cannot inspect the retained image's outbox contract"
[[ "$compatible" == true ]] || die "retained image does not declare the text-file-v1 outbox contract; recovery pair retained"
python3 "$pair_tool" check "$service" "$previous"
overlay="$previous/50-worker-profile.conf"
if [[ "${release##*/}" == "$service-v2-"* ]]; then
  overlay="$release/rollback.conf"
  [[ -f "$release/previous.endpoint" && ! -L "$release/previous.endpoint" ]] ||
    die "captured rollback endpoint is missing"
  hashes="$(cd "$release" && sha256sum previous/pair.json previous.endpoint rollback.conf)"
  cmp -s "$release/rollback.sha256" <(printf '%s\n' "$hashes") ||
    die "retained rollback overlay or observed endpoint changed"
fi
python3 "$pair_tool" verify-dropin "$service" "$previous" "$overlay"
restore_unit=false
if [[ -f "$previous/unit.container" ]] && ! cmp -s -- "$previous/unit.container" "$installed_unit"; then
  restore_unit=true
elif [[ ! -f "$previous/unit.container" ]]; then
  echo "[WARN] $previous has no recorded base unit; the installed unit definition stays as is" >&2
fi
recorded_unit="$previous/unit.container"
[[ -f "$recorded_unit" ]] || recorded_unit="$installed_unit"
compose_env="$prefix/etc/stack-secrets/hololive-bot/compose.env"
current_endpoint="$(postgres_endpoint_quadlet "$installed_unit" "$compose_env")" || die "cannot determine current effective PostgreSQL endpoint"
previous_endpoint="$(postgres_endpoint_quadlet "$recorded_unit" "$compose_env" "$installed_unit")" || die "cannot determine rollback effective PostgreSQL endpoint"
if [[ "$overlay" == "$release/rollback.conf" ]]; then
  cmp -s "$release/previous.endpoint" <(printf '%s\n' "$previous_endpoint") ||
    die "captured running endpoint differs from the rollback configuration; retain the recovery pair"
fi
postgres_endpoint_require_same "$current_endpoint" "$previous_endpoint" ||
  die "rollback not applied; current service state unchanged; recovery pair retained: $previous"
postgres_capacity_assert_service "$ROOT_DIR" "$compose_env" "$(dirname "$installed_unit")" "$service" "$recorded_unit"
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
python3 "$pair_tool" activate "$service" "$previous" "$deploy_root" "$overlay"
systemctl daemon-reload
systemctl restart "$unit"
systemctl is-active --quiet "$unit" || die "$unit is not active after restart"
python3 "$pair_tool" verify-live "$service" "$previous" "$service"
restarts="$(systemctl show "$unit" -p NRestarts --value)"
[[ "$restarts" == 0 ]] || die "$unit restarted automatically after rollback (NRestarts=$restarts)"
echo "[ROLLBACK] $service restored from $previous"
