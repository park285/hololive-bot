#!/usr/bin/env bash
set -Eeuo pipefail

die() { echo "[quadlet-rollback] $*" >&2; exit 1; }
usage() { echo "Usage: $0 <release-dir> [--check|--apply]" >&2; exit 2; }

[[ $# -ge 1 && $# -le 2 ]] || usage
mode="${2:---check}"
[[ "$mode" == --check || "$mode" == --apply ]] || usage

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  exec "$(dirname -- "$0")/quadlet-redeploy-service.sh" --rollback "$@"
fi

. "$ROOT_DIR/scripts/deploy/lib/compose-env.sh"
. "$ROOT_DIR/scripts/deploy/lib/postgres-endpoint.sh"
. "$ROOT_DIR/scripts/deploy/lib/postgres-capacity.sh"

release="$(realpath -e -- "$1")" || die "release directory not found: $1"
[[ "$release" == "$deploy_root/artifacts/worker-profile/releases/"* ]] ||
  die "release must live under $deploy_root/artifacts/worker-profile/releases"
if [[ "$sealed" == true ]]; then
  [[ "$release" == "$1" && "${release%/*}" == "$releases" ]] || die 'noncanonical retained app release'
  sealed_authority tree "$release"
fi
service="$(<"$release/service")"
case "$service" in
  hololive-api|hololive-alarm-worker) ;;
  *) die "unsupported service recorded in $release/service" ;;
esac
unit="$service.service"
previous="$release/previous"
installed_unit="$prefix/etc/containers/systemd/hololive-bot/$service.container"
if [[ "$sealed" == true ]]; then
  sealed_begin_slot "$service"
  pending_original=false
  if [[ -f "$release/activation.intent" && -n "$original_target_generation" ]] &&
    cmp -s "$release/before.generation" <(printf '%s\n' "$original_target_generation"); then
    sealed_check_units "$service" "$release"
    pending_original=true
  else
    sealed_check_units "$service"
    cmp -s "$deploy_root/artifacts/worker-profile/active/$service.pair" <(printf '%s/candidate\n' "$release") ||
      die 'requested release is not the installed pair for this service'
  fi
  cmp -s "$release/candidate/unit.container" "$installed_unit" || die 'installed app base differs from the retained candidate'
  candidate="$(<"$release/candidate.image")"
  candidate_revision="$(<"$release/candidate.revision")"
  [[ "$candidate" =~ ^sha256:[0-9a-f]{64}$ && "$candidate_revision" =~ ^[0-9a-f]{40}$ ]] &&
    cmp -s "$release/candidate.image" <(printf '%s\n' "$candidate") &&
    cmp -s "$release/candidate.revision" <(printf '%s\n' "$candidate_revision") &&
    [[ "$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$candidate")" == "$candidate_revision" &&
       "$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.title"}}' "$candidate")" == "$service" ]] ||
    die 'retained app provenance mismatch'
  python3 "$pair_tool" verify "$service" "$release/candidate" "$candidate"
  if [[ "$pending_original" == true ]]; then
    [[ "$candidate_revision" == "$revision" ]] || die 'partial activation belongs to a different source'
  fi
  assert_recovery_target() {
    [[ -n "$original_target_generation" && "$(sealed_generation "$service")" == "$original_target_generation" ]] || return 1
    if [[ "$pending_original" == true ]]; then
      python3 "$pair_tool" verify-captured "$service" "$release/previous" "$service"
    else
      if [[ -f "$release/activation.intent" ]]; then
        local name id rest
        read -r name id rest <<<"$original_target_generation"
        [[ "$name" == "$service" && -n "$rest" ]] && cmp -s "$release/writer-job/invocation" <(printf '%s\n' "$id") || return 1
      fi
      python3 "$pair_tool" verify-live "$service" "$release/candidate" "$service"
    fi
  }
  assert_recovery_target || die '[OUTCOME] outcome_unknown: writer ownership unconfirmed; do not start or retry'
  protected_before="$(sealed_bind_generation holo-postgres && sealed_bind_generation holo-pgbouncer)"
fi
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
if [[ "$sealed" == true ]]; then
  [[ "$(podman image inspect --format '{{index .Config.Labels "org.opencontainers.image.title"}}' "$previous_image")" == "$service" &&
     "$(podman image inspect --format '{{.Os}}/{{.Architecture}}' "$previous_image")" == linux/arm64 ]] ||
    die 'retained recovery image is not the approved service or platform'
fi
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
check_installed_endpoints() {
  local current_endpoint previous_endpoint
  current_endpoint="$(postgres_endpoint_quadlet "$installed_unit" "$compose_env")" || die "cannot determine current effective PostgreSQL endpoint"
  previous_endpoint="$(postgres_endpoint_quadlet "$recorded_unit" "$compose_env" "$installed_unit")" || die "cannot determine rollback effective PostgreSQL endpoint"
  if [[ "$overlay" == "$release/rollback.conf" ]]; then
    cmp -s "$release/previous.endpoint" <(printf '%s\n' "$previous_endpoint") ||
      die "captured running endpoint differs from the rollback configuration; retain the recovery pair"
  fi
  postgres_endpoint_require_same "$current_endpoint" "$previous_endpoint" ||
    die "rollback not applied; current service state unchanged; recovery pair retained: $previous"
  postgres_capacity_assert_service "$ROOT_DIR" "$compose_env" "$(dirname "$installed_unit")" "$service" "$recorded_unit"
}
if [[ "$sealed" == true && "$pending_original" == true ]]; then
  [[ "$restore_unit" == false ]] && cmp -s "$release/previous.endpoint" <(printf 'holo-pgbouncer|6432\n') ||
    die 'original app configuration not confirmed'
  postgres_pooler_assert_writer_sql "$service"
else
  check_installed_endpoints
fi
if [[ "$sealed" == true ]]; then
  sealed_assert_peer
  assert_recovery_target || die 'selected app changed during recovery checks'
  protected_after="$(sealed_bind_generation holo-postgres && sealed_bind_generation holo-pgbouncer)"
  [[ "$protected_before" == "$protected_after" ]] || die 'protected bind changed during recovery checks'
  recovery_source="$(sealed_recovery_prestart "$previous")" || die 'retained prestart provenance unconfirmed'
  if [[ "$pending_original" == true ]]; then
    no_writer_job() {
      local job
      [[ ! -e "$release/writer-job" && ! -L "$release/writer-job" ]] &&
        job="$(systemctl show "$unit" -p Job --value)" && [[ -z "$job" ]]
    }
    if ! no_writer_job; then
      [[ "$mode" == --check ]] || die '[OUTCOME] outcome_unknown: writer job not excluded; --apply refused'
      echo "[CHECK_ONLY] captured writer and previous pair verified; job outcome unknown, --apply refused: $release"
      exit 0
    fi
  fi
fi
if [[ "$mode" == --check ]]; then
  [[ "$restore_unit" != true ]] || echo "[CHECK] --apply also restores the base unit the previous image ran with: $installed_unit"
  echo "[CHECK] $service previous pair verified: $previous"
  exit 0
fi

for marker in fence.intent fenced; do
  [[ ! -e "$fence_dir/$marker" ]] || die "primary fence marker present: $fence_dir/$marker"
done
if [[ "$sealed" == true ]]; then
  mkdir -m0700 "$release/recovery-attempt" || die '[OUTCOME] outcome_unknown: a recovery was already attempted; do not retry'
  printf '%s\n%s\n' "$original_target_generation" "$previous_image" >"$release/recovery-attempt/request"
  sync -f "$release"
  trap 'rc=$?; if (( rc != 0 )); then echo "[OUTCOME] outcome_unknown: retain recovery-attempt and inspect the writer; do not retry" >&2; fi' EXIT
fi
if [[ "$sealed" == true && "$pending_original" == true ]]; then
  mkdir -m0700 "$release/restore-original" || die 'previous recovery intent exists; inspect without retry'
  sync -f "$release"
  before="$(<"$release/before.pair")"
  no_writer_job && assert_recovery_target || die '[OUTCOME] outcome_unknown: original writer no longer idle'
  python3 "$pair_tool" activate "$service" "$before" "$deploy_root"
  systemctl daemon-reload
  sealed_loaded "$service" "$previous_image" "$recovery_source"
  check_installed_endpoints
  assert_recovery_target && sealed_assert_peer || die '[OUTCOME] outcome_unknown: original writer changed during recovery'
  echo "[ROLLBACK] $service original installed settings restored without restarting its captured writer"
  exit 0
fi
if [[ "$restore_unit" == true ]]; then
  cp -p -- "$installed_unit" "$release/rollback-replaced-$service.container"
  unit_tmp="$(mktemp "$(dirname -- "$installed_unit")/.$service.container.XXXXXX")"
  install -m 0644 -- "$previous/unit.container" "$unit_tmp"
  mv -f -- "$unit_tmp" "$installed_unit"
  echo "[ROLLBACK] restored the base unit the previous image ran with; run sync-opt-current.sh for the intended revision before the next deployment"
fi
python3 "$pair_tool" activate "$service" "$previous" "$deploy_root" "$overlay"
systemctl daemon-reload
if [[ "$sealed" == true ]]; then
  sealed_loaded "$service" "$previous_image" "$recovery_source"
  sealed_single_job restart "$unit" "$previous_image" "$release/rollback-job"
else
  systemctl restart "$unit"
fi
systemctl is-active --quiet "$unit" || die "$unit is not active after restart"
python3 "$pair_tool" verify-live "$service" "$previous" "$service"
if [[ "$sealed" == true ]]; then
  sealed_accept "$service" "$previous_image"
  sealed_assert_peer
  protected_after="$(sealed_bind_generation holo-postgres && sealed_bind_generation holo-pgbouncer)"
  [[ "$protected_after" == "$protected_before" ]] || die 'protected bind changed during recovery'
fi
restarts="$(systemctl show "$unit" -p NRestarts --value)"
[[ "$restarts" == 0 ]] || die "$unit restarted automatically after rollback (NRestarts=$restarts)"
echo "[ROLLBACK] $service restored from $previous"
