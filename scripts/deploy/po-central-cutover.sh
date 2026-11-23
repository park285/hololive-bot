#!/usr/bin/env bash
set -Eeuo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
[[ "$(hostname -s)" == kapu ]] || { echo 'image build/export is restricted to kapu' >&2; exit 1; }
usage() { echo "Usage: $0 <c|b> deploy | c quiesce | <c|b> quiesce-verified|verify-pair <local-pair-dir> <trusted-transport-sha256> | b preflight|quiesce <clean-candidate-tree> <trusted-receipt-dir> <b-staging-timestamp> <owner-window-id> <trusted-origin-pair-dir> <prepared-resume-grant> | b resume|check|rollback <clean-candidate-tree> <trusted-receipt-dir> <b-staging-timestamp> <owner-window-id> <trusted-origin-pair-dir> | <c|b> resume|rollback|check <card-backup>" >&2; exit 2; }
[[ $# -ge 2 && $# -le 8 ]] || usage
slot="$1"
shift
. "$root/scripts/deploy/lib/collector-slots.sh"
[[ "$(collector_slot "$slot" deploy 2>/dev/null)" == pair ]] || usage
remote_root="$(collector_slot "$slot" root)"
slot_preamble="$(collector_slot_preamble "$slot")"
slot_upper="${slot^^}"
source_payload_files() {
  local tree="$1" receipts name
  cat "$tree/scripts/deploy/po-central-files.txt" || return 1
  . "$root/scripts/deploy/lib/migration-receipts.sh"
  receipts="$(migration_receipts "$tree")" || return 1
  while read -r _ name; do
    if [[ "$name" != 001_schema_epoch4_baseline.sql ]]; then
      printf 'hololive/hololive-api/scripts/migrations/%s\n' "$name" "contracts/$name"
    fi
  done <<<"$receipts"
}
pair_files_regular() {
  local file
  for file in transport-files.sha256 source-files.sha256 rootfs-manifest.json collector-manifest.json image-id collector-image-id \
    image.tar image.tar.sha256 collector-image.tar collector-image.tar.sha256; do
    [[ -f "$file" && ! -L "$file" ]] || return 1
  done
}
archive_sums_match() {
  local archive
  for archive in image.tar collector-image.tar; do
    sha256sum -- "$archive" | cmp -s -- - "$archive.sha256" || return 1
  done
}
ssh_var="PO_${slot_upper}_SSH_TARGET" deploy_var="APPROVE_PO_${slot_upper}_DEPLOY" rollback_var="APPROVE_PO_${slot_upper}_ROLLBACK"
mode="$1"
verified_pair='' backup_prefix="card-po-$slot"
remote_mode="$mode" seal=-
if [[ "$slot" == b && ( "$mode" =~ ^(preflight|quiesce)$ || ( "$mode" =~ ^(resume|check|rollback)$ && $# != 2 ) ) ]]; then
  b_args=6
  if [[ "$mode" =~ ^(preflight|quiesce)$ ]]; then b_args=7; fi
  [[ $# == "$b_args" && "$4" =~ ^[0-9]{8}T[0-9]{6}Z$ && "$5" =~ ^[0-9a-f]{32}$ ]] || usage
  resume_grant="${7-}"
  [[ "${!ssh_var:-}" == iris-seoul ]]
  case "$mode" in
    rollback) [[ "${!rollback_var:-}" == true ]] || exit 2 ;;
    quiesce|resume) [[ "${!deploy_var:-}" == true ]] || exit 2 ;;
  esac
  [[ "$root" == "$(git -C "$root" rev-parse --show-toplevel)" &&
     "$(realpath -e -- "${BASH_SOURCE[0]}")" == "$root/scripts/deploy/po-central-cutover.sh" ]]
  tool_revision="$(git -C "$root" rev-parse --verify 'HEAD^{commit}')"
  for script in scripts/deploy/po-central-cutover.sh scripts/deploy/lib/source-revision.sh scripts/deploy/lib/po-central-remote.sh \
    scripts/deploy/lib/collector-slots.sh scripts/deploy/lib/collector-slots.tsv scripts/deploy/lib/migration-receipts.sh; do
    git -C "$root" cat-file blob "$tool_revision:$script" | cmp -s -- - "$root/$script"
  done
  . "$root/scripts/deploy/lib/source-revision.sh"
  [[ "$(deploy_source_revision "$root")" == "$tool_revision" ]]
  source_root="$(realpath -e -- "$2")"
  [[ "$source_root" == "$(git -C "$source_root" rev-parse --show-toplevel)" ]]
  revision="$(deploy_source_revision "$source_root")"
  receipt_dir="$(realpath -e -- "$3")"
  [[ "$receipt_dir" == "$3" && "$(stat -c '%u:%a' "$receipt_dir")" == "$EUID:700" ]]
  change_id="$4" window="$5"
  w2_pair="$(realpath -e -- "$6")"
  grant_name=b.current-grant
  grant_phase="$mode"
  if [[ "$mode" == check ]]; then grant_name="b.quiesce.$change_id.requested" grant_phase=quiesce; fi
  if [[ "$mode" == preflight ]]; then grant_phase=quiesce; fi
  receipts=("b.stage.$change_id.finished" "$grant_name")
  [[ "$mode" =~ ^(preflight|quiesce)$ ]] || receipts+=(c.accepted)
  for receipt in "${receipts[@]}"; do
    [[ -f "$receipt_dir/$receipt" && ! -L "$receipt_dir/$receipt" &&
       "$(stat -c '%u:%a' "$receipt_dir/$receipt")" == "$EUID:600" ]] || { echo "missing or untrusted $receipt" >&2; exit 1; }
  done
  read -r staged_revision seal staged_change extra < "$receipt_dir/b.stage.$change_id.finished"
  [[ "$staged_revision" == "$revision" && "$seal" =~ ^[0-9a-f]{64}$ && "$staged_change" == "$change_id" && -z "$extra" ]]
  cmp -s -- "$receipt_dir/b.stage.$change_id.finished" <(printf '%s %s %s\n' "$revision" "$seal" "$change_id")
  if [[ ! "$mode" =~ ^(preflight|quiesce)$ ]]; then cmp -s -- "$receipt_dir/c.accepted" <(printf '%s %s\n' "$revision" "$seal"); fi
  grant="$receipt_dir/$grant_name"
  if [[ "$mode" != check ]]; then [[ "$(stat -c '%h' "$grant")" == 1 ]]; fi
  read -r -a fields < "$grant"
  [[ "${#fields[@]}" == 13 ]]
  w2_revision="${fields[8]}" w2_collector="${fields[9]}" w2_issuer="${fields[10]}" origin_digest="${fields[11]}" expires="${fields[12]}"
  [[ "$w2_revision" =~ ^[0-9a-f]{40}$ && "$w2_collector" =~ ^sha256:[0-9a-f]{64}$ &&
     "$w2_issuer" =~ ^sha256:[0-9a-f]{64}$ && "$origin_digest" =~ ^[0-9a-f]{64}$ ]]
  [[ "$expires" =~ ^[1-9][0-9]{0,11}$ ]]
  if [[ "$mode" != check ]]; then (( EPOCHSECONDS < expires )); fi
  for receipt in collector-image-id image-id collector-manifest.json rootfs-manifest.json; do
    [[ -f "$w2_pair/$receipt" && ! -L "$w2_pair/$receipt" ]]
  done
  cmp -s -- "$w2_pair/collector-image-id" <(printf '%s\n' "$w2_collector")
  cmp -s -- "$w2_pair/image-id" <(printf '%s\n' "$w2_issuer")
  jq -e --arg r "$w2_revision" '.source_revision==$r and .go.goarch=="arm64"' "$w2_pair/collector-manifest.json" >/dev/null
  jq -e --arg r "$w2_revision" '.source_revision==$r and .architecture=="arm64"' "$w2_pair/rootfs-manifest.json" >/dev/null
  grant_pair="$change_id $tool_revision $revision $seal $w2_revision $w2_collector $w2_issuer $origin_digest"
  expected_grant="PO_B_PAIR_V1 iris-seoul $window $grant_phase $grant_pair $expires"
  cmp -s -- "$grant" <(printf '%s\n' "$expected_grant")
  if [[ "$mode" == resume ]]; then
    quiesced="$receipt_dir/b.$window.quiesce"
    [[ -f "$quiesced.used" && ! -L "$quiesced.used" && ! -L "$quiesced.finished" && "$quiesced.used" -ef "$quiesced.finished" ]]
    cmp -s -- "$quiesced.used" <(printf '%s\n' "${expected_grant/ resume / quiesce }")
  elif [[ "$mode" == rollback ]]; then
    previous="$receipt_dir/b.quiesce.$change_id.requested"
    [[ -f "$previous" && ! -L "$previous" ]]
    read -r -a origin < "$previous"
    [[ "${#origin[@]}" == 13 && "${origin[2]}" =~ ^[0-9a-f]{32}$ && "${origin[12]}" =~ ^[1-9][0-9]{0,11}$ ]]
    finished="$receipt_dir/b.${origin[2]}.quiesce.finished"
    [[ ! -L "$finished" && "$previous" -ef "$finished" ]]
    cmp -s -- "$previous" <(printf 'PO_B_PAIR_V1 iris-seoul %s quiesce %s %s\n' "${origin[2]}" "$grant_pair" "${origin[12]}")
  fi
  archive="$receipt_dir/arm64"
  [[ -d "$archive" && ! -L "$archive" ]]
  (
    cd -- "$archive"
    pair_files_regular
    printf '%s  transport-files.sha256\n' "$seal" | sha256sum --check --strict --status -
    sha256sum source-files.sha256 rootfs-manifest.json collector-manifest.json image-id collector-image-id \
      image.tar.sha256 collector-image.tar.sha256 | cmp -s -- - transport-files.sha256
    archive_sums_match
  )
  files=scripts/deploy/po-central-files.txt
  version_file=hololive/hololive-api/VERSION
  for file in "$files" "$version_file"; do
    git -C "$source_root" cat-file blob "$revision:$file" | cmp -s -- - "$source_root/$file"
  done
  sources="$(source_payload_files "$source_root")"
  (
    cd -- "$source_root"
    while IFS= read -r file; do
      git cat-file blob "$revision:$file" | cmp -s -- - "$file" || exit 1
      sha256sum -- "$file" || exit 1
    done <<< "$sources"
  ) | cmp -s -- - "$archive/source-files.sha256"
  version="$(git -C "$source_root" cat-file blob "$revision:$version_file")"
  [[ "$version" =~ ^[A-Za-z0-9._-]+$ ]]
  backup="$remote_root/backups/staged-po-b-$change_id"
  staging="$remote_root/po-staging-$change_id"
  remote_body="$slot_preamble"$'\n'"$(git -C "$root" cat-file blob "$tool_revision:scripts/deploy/lib/po-central-remote.sh")"
  if [[ "$mode" != check ]]; then
    used="$receipt_dir/b.$window.$grant_phase.used"
    requested="$receipt_dir/b.$grant_phase.$change_id.requested"
    if [[ "$mode" != preflight ]]; then
      exec 9>>"$receipt_dir/receipts.lock"
      flock -n 9 || { echo 'another grant publication or consumption holds the receipt lock' >&2; exit 1; }
    fi
    [[ ! -e "$requested" && ! -L "$requested" && ! -e "$used" && ! -L "$used" ]] || { echo "b $grant_phase was already requested for this stage or window" >&2; exit 1; }
    [[ -f "$grant" && ! -L "$grant" && "$(stat -c '%u:%a:%h' "$grant")" == "$EUID:600:1" ]] || { echo 'window grant withdrawn or replaced' >&2; exit 1; }
    cmp -s -- "$grant" <(printf '%s\n' "$expected_grant") || { echo 'window grant withdrawn or replaced' >&2; exit 1; }
    (( EPOCHSECONDS < expires ))
    if [[ "$grant_phase" == quiesce ]]; then
      (( expires - EPOCHSECONDS >= 3600 )) || { echo 'quiesce grant must stay valid for at least an hour so the same expiry covers b resume' >&2; exit 1; }
      cmp -s -- "$resume_grant" <(printf '%s\n' "${expected_grant/ quiesce / resume }") ||
        { echo 'prepared resume grant must be this quiesce grant with only its phase changed to resume' >&2; exit 1; }
    fi
    if [[ "$mode" == preflight ]]; then
      printf 'collector b preflight: source=%s seal=%s window=%s\n' "$revision" "$seal" "$window"
      exit 0
    fi
    ln -T -- "$grant" "$requested"
    ln -T -- "$grant" "$used"
    cmp -s -- "$used" <(printf '%s\n' "$expected_grant")
    sync -- "$used" "$receipt_dir"
    [[ "$used" -ef "$grant" && ! -L "$grant" ]] || { echo 'window grant withdrawn or replaced' >&2; exit 1; }
    rm -- "$grant"
    sync -- "$receipt_dir"
    (( EPOCHSECONDS < expires ))
    exec 9>&-
  fi
  ssh "${!ssh_var}" sudo -n bash -s -- b "$mode" "$staging" "$backup" "$revision" "$version" "$seal" \
    "$window" "$w2_revision" "$w2_collector" "$w2_issuer" "$origin_digest" "$expires" "$tool_revision" \
    <<< "$remote_body"
  if [[ "$mode" != check ]]; then
    ln -T -- "$used" "$receipt_dir/b.$window.$mode.finished"
    sync -- "$receipt_dir"
  fi
  printf 'collector b %s: source=%s backup=%s staging=%s\n' "$mode" "$revision" "$backup" "$staging"
  exit 0
fi
. "$root/scripts/deploy/lib/source-revision.sh"
version="$(cat "$root/hololive/hololive-api/VERSION")"
[[ "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 1
verify_local_pair() {
  local pair="$1" pair_seal="$2" pair_revision="$3" sources="$4" expected_sources
  (
    cd -- "$pair" || exit 1
    pair_files_regular || exit 1
    printf '%s  transport-files.sha256\n' "$pair_seal" | sha256sum --check --strict --status - || exit 1
    actual="$(sha256sum source-files.sha256 rootfs-manifest.json collector-manifest.json image-id collector-image-id image.tar.sha256 collector-image.tar.sha256)" || exit 1
    cmp -s transport-files.sha256 <(printf '%s\n' "$actual") || exit 1
    archive_sums_match || exit 1
    jq -e --arg r "$pair_revision" '.source_revision==$r and .go.goarch=="arm64"' collector-manifest.json >/dev/null || exit 1
    jq -e --arg r "$pair_revision" '.source_revision==$r and .architecture=="arm64"' rootfs-manifest.json >/dev/null
  ) || { echo 'verified pair transport or revision mismatch; nothing transferred' >&2; return 1; }
  expected_sources="$(cd "$root"; while IFS= read -r file; do sha256sum "$file" || exit 1; done <<<"$sources")"
  cmp -- "$pair/source-files.sha256" <(printf '%s\n' "$expected_sources")
}
if [[ "$mode" == verify-pair ]]; then
  [[ $# == 3 && "$3" =~ ^[0-9a-f]{64}$ ]] || usage
  revision="$(deploy_source_revision "$root")"
  payload_files="$(source_payload_files "$root")"
  verify_local_pair "$(realpath -e -- "$2")" "$3" "$revision" "$payload_files"
  printf 'collector %s pair verified: source=%s seal=%s\n' "$slot" "$revision" "$3"
  exit 0
fi
if [[ "$mode" == quiesce-verified ]]; then
  [[ $# == 3 && "$3" =~ ^[0-9a-f]{64}$ ]] || usage
  verified_pair="$(realpath -e -- "$2")"
  [[ -d "$verified_pair" ]] || usage
  seal="$3"
  mode=quiesce
  set -- quiesce
fi
[[ "$mode" == deploy || "$mode" == quiesce || "$mode" == resume || "$mode" == rollback || "$mode" == check ]] || usage
ssh_target="${!ssh_var:-}"
[[ -n "$ssh_target" ]] || { echo "set $ssh_var to the approved collector $slot SSH host" >&2; exit 2; }
[[ "$ssh_target" =~ ^[A-Za-z0-9@._-]+$ ]] || exit 2
if [[ "$mode" == deploy || "$mode" == quiesce ]]; then
  [[ $# -eq 1 && "${!deploy_var:-}" == true ]] || { echo "$deploy_var=true required" >&2; exit 2; }
elif [[ "$mode" == resume ]]; then
  [[ $# -eq 2 && "${!deploy_var:-}" == true ]] || { echo "$deploy_var=true required" >&2; exit 2; }
elif [[ "$mode" == rollback ]]; then
  [[ $# -eq 2 && "${!rollback_var:-}" == true ]] || { echo "$rollback_var=true required" >&2; exit 2; }
else
  [[ $# -eq 2 ]] || exit 2
fi
build_local_pair() {
  local_stage="$(mktemp -d)"
  trap 'rm -rf "$local_stage"' EXIT
  "$root/scripts/build/build-po-sandbox-artifact.sh" arm64 "$revision" "$version" "$local_stage/issuer" podman
  collector_image="$(collector_slot "$slot" collector_image):candidate"
  podman build --platform linux/arm64 --format docker \
    --tag "$collector_image" \
    --file "$root/hololive/hololive-collector/Dockerfile" \
    --build-arg "REVISION=$revision" --label "org.opencontainers.image.revision=$revision" \
    --build-arg "VERSION=$version" "$root"
  [[ "$(podman image inspect -f '{{.Os}}/{{.Architecture}}' "$collector_image")" == linux/arm64 ]]
  [[ "$(podman image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$collector_image")" == "$revision" ]]
  collector_id="$(podman create "$collector_image")"
  podman cp "$collector_id:/app/bin/hololive-collector" "$local_stage/collector-binary"
  podman cp "$collector_id:/app/manifest.json" "$local_stage/collector-manifest.json"
  podman rm "$collector_id" >/dev/null
  python3 - "$local_stage/collector-manifest.json" "$local_stage/collector-binary" "$revision" <<'PY'
import hashlib, json, sys
m=json.load(open(sys.argv[1])); assert m['source_revision'] == sys.argv[3] and m['go']['goarch'] == 'arm64'
assert hashlib.sha256(open(sys.argv[2], 'rb').read()).hexdigest() == m['files']['bin/hololive-collector']
PY
  podman save --format docker-archive --output "$local_stage/collector-image.tar" "$collector_image"
  (cd "$local_stage" && sha256sum collector-image.tar > collector-image.tar.sha256)
  python3 "$root/scripts/build/po-sandbox-manifest.py" image-ids "$local_stage/collector-image.tar" \
    "sha256:$(podman image inspect -f '{{.Id}}' "$collector_image")" > "$local_stage/collector-image-id"
  (cd "$root"
    while IFS= read -r file; do sha256sum "$file"; done <<<"$payload_files"
  ) > "$local_stage/source-files.sha256"
}

if [[ "$mode" == deploy || "$mode" == quiesce ]]; then
  payload_files="$(source_payload_files "$root")"
  revision="$(deploy_source_revision "$root")"
  change_id="$(date -u +%Y%m%dT%H%M%SZ)"
  backup="$remote_root/backups/$backup_prefix-$change_id"
  staging="$remote_root/po-staging-$change_id"
  if [[ -n "$verified_pair" ]]; then
    local_stage="$verified_pair"
    issuer_dir="$verified_pair"
    verify_local_pair "$verified_pair" "$seal" "$revision" "$payload_files"
  else
    build_local_pair
    issuer_dir="$local_stage/issuer"
  fi
  ssh "$ssh_target" "sudo -n mkdir -m 0700 -- '$staging'"
  rsync -a --rsync-path='sudo -n rsync' \
    --files-from=<(printf '%s\n' "$payload_files") "$root/" "$ssh_target:$staging/"
  rsync -a --rsync-path='sudo -n rsync' \
    "$issuer_dir/image.tar" "$issuer_dir/image.tar.sha256" \
    "$issuer_dir/image-id" "$issuer_dir/rootfs-manifest.json" "$local_stage/collector-image.tar" \
    "$local_stage/collector-image.tar.sha256" "$local_stage/collector-image-id" "$local_stage/collector-manifest.json" \
    "$local_stage/source-files.sha256" \
    "$ssh_target:$staging/"
  if [[ -n "$verified_pair" ]]; then
    rsync -a --rsync-path='sudo -n rsync' "$local_stage/transport-files.sha256" "$ssh_target:$staging/"
  fi
  ssh "$ssh_target" "sudo -n chown -R root:root '$staging' && sudo -n chmod -R go-w '$staging'"
else
  backup="$2"
  [[ "$backup" =~ ^$remote_root/backups/$backup_prefix-[0-9]{8}T[0-9]{6}Z$ ]] || exit 2
  change_id="${backup##*-}"
  staging="$remote_root/po-staging-$change_id"
  revision="$(ssh "$ssh_target" "sudo -n cat '$backup/candidate.revision'")"
  version="$(ssh "$ssh_target" "sudo -n cat '$backup/candidate.version'")"
  [[ "$revision" =~ ^[0-9a-f]{40}$ && "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 1
  if [[ "$mode" == resume ]]; then
    [[ "$(deploy_source_revision "$root")" == "$revision" ]] || { echo 'resume source differs from retained candidate' >&2; exit 1; }
  fi
fi
printf '%s\n' "$slot_preamble" | cat - "$root/scripts/deploy/lib/po-central-remote.sh" |
  ssh "$ssh_target" sudo -n bash -s -- "$slot" "$remote_mode" "$staging" "$backup" "$revision" "$version" "$seal"
printf 'collector %s %s: source=%s backup=%s staging=%s\n' "$slot" "$remote_mode" "$revision" "$backup" "$staging"
