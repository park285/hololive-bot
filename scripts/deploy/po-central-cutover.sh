#!/usr/bin/env bash
set -Eeuo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$root/scripts/deploy/lib/source-revision.sh"
[[ "$(hostname -s)" == kapu ]] || { echo 'image build/export is restricted to kapu' >&2; exit 1; }
usage() { echo "Usage: $0 <c|b> deploy|quiesce | <c|b> quiesce-verified <local-pair-dir> <trusted-transport-sha256> | <c|b> resume|rollback|check <remote-root>/backups/card-po-<slot>-TIMESTAMP" >&2; exit 2; }
[[ $# -ge 2 && $# -le 4 ]] || usage
slot="$1"
shift
case "$slot" in
  c) remote_root=/opt/hololive-bot/compose ;;
  b) remote_root=/opt/hololive-bot/collector-b ;;
  *) usage ;;
esac
slot_upper="${slot^^}"
ssh_var="PO_${slot_upper}_SSH_TARGET" deploy_var="APPROVE_PO_${slot_upper}_DEPLOY" rollback_var="APPROVE_PO_${slot_upper}_ROLLBACK"
mode="$1"
verified_pair=
remote_mode="$mode" seal=-
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
version="$(cat "$root/hololive/hololive-api/VERSION")"
[[ "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 1
build_local_pair() {
  local_stage="$(mktemp -d)"
  trap 'rm -rf "$local_stage"' EXIT
  "$root/scripts/build/build-po-sandbox-artifact.sh" arm64 "$revision" "$version" "$local_stage/issuer" podman
  collector_image=localhost/hololive-youtube-collector:candidate
  podman build --platform linux/arm64 --format docker \
    --tag "$collector_image" \
    --file "$root/hololive/hololive-youtube-collector/Dockerfile" \
    --build-arg "REVISION=$revision" --label "org.opencontainers.image.revision=$revision" \
    --build-arg "VERSION=$version" "$root"
  [[ "$(podman image inspect -f '{{.Os}}/{{.Architecture}}' "$collector_image")" == linux/arm64 ]]
  [[ "$(podman image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$collector_image")" == "$revision" ]]
  collector_id="$(podman create "$collector_image")"
  podman cp "$collector_id:/app/bin/youtube-collector" "$local_stage/collector-binary"
  podman cp "$collector_id:/app/manifest.json" "$local_stage/collector-manifest.json"
  podman rm "$collector_id" >/dev/null
  python3 - "$local_stage/collector-manifest.json" "$local_stage/collector-binary" "$revision" <<'PY'
import hashlib, json, sys
m=json.load(open(sys.argv[1])); assert m['source_revision'] == sys.argv[3] and m['go']['goarch'] == 'arm64'
assert hashlib.sha256(open(sys.argv[2], 'rb').read()).hexdigest() == m['files']['bin/youtube-collector']
PY
  podman save --format docker-archive --output "$local_stage/collector-image.tar" "$collector_image"
  (cd "$local_stage" && sha256sum collector-image.tar > collector-image.tar.sha256)
  python3 "$root/scripts/build/po-sandbox-manifest.py" image-ids "$local_stage/collector-image.tar" \
    "sha256:$(podman image inspect -f '{{.Id}}' "$collector_image")" > "$local_stage/collector-image-id"
  # 이미지와 같은 검토 source의 bounded 입력만 전송하고, 재개 때도 이 바이트를 대조한다.
  (cd "$root"
    while IFS= read -r file; do sha256sum "$file"; done < scripts/deploy/po-central-files.txt
  ) > "$local_stage/source-files.sha256"
}

if [[ "$mode" == deploy || "$mode" == quiesce ]]; then
  revision="$(deploy_source_revision "$root")"
  change_id="$(date -u +%Y%m%dT%H%M%SZ)"
  backup="$remote_root/backups/card-po-$slot-$change_id"
  staging="$remote_root/po-staging-$change_id"
  if [[ -n "$verified_pair" ]]; then
    local_stage="$verified_pair"
    issuer_dir="$verified_pair"
    (
      cd -- "$verified_pair" || exit 1
      printf '%s  transport-files.sha256\n' "$seal" | sha256sum --check --strict --status - || exit 1
      actual="$(sha256sum source-files.sha256 rootfs-manifest.json collector-manifest.json image-id collector-image-id image.tar.sha256 collector-image.tar.sha256)" || exit 1
      cmp -s transport-files.sha256 <(printf '%s\n' "$actual") || exit 1
      sha256sum --check --strict --status image.tar.sha256 collector-image.tar.sha256 || exit 1
      jq -e --arg r "$revision" '.source_revision==$r and .go.goarch=="arm64"' collector-manifest.json >/dev/null || exit 1
      jq -e --arg r "$revision" '.source_revision==$r and .architecture=="arm64"' rootfs-manifest.json >/dev/null
    ) || { echo 'verified pair transport or revision mismatch; nothing transferred' >&2; exit 1; }
    expected_sources="$(cd "$root"; while IFS= read -r file; do sha256sum "$file" || exit 1; done <scripts/deploy/po-central-files.txt)"
    cmp -- "$local_stage/source-files.sha256" <(printf '%s\n' "$expected_sources")
  else
    build_local_pair
    issuer_dir="$local_stage/issuer"
  fi
  ssh "$ssh_target" "sudo -n mkdir -m 0700 -- '$staging'"
  rsync -a --rsync-path='sudo -n rsync' \
    --files-from="$root/scripts/deploy/po-central-files.txt" "$root/" "$ssh_target:$staging/"
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
  [[ "$backup" =~ ^$remote_root/backups/card-po-$slot-[0-9]{8}T[0-9]{6}Z$ ]] || exit 2
  change_id="${backup##*-}"
  staging="$remote_root/po-staging-$change_id"
  revision="$(ssh "$ssh_target" "sudo -n cat '$backup/candidate.revision'")"
  version="$(ssh "$ssh_target" "sudo -n cat '$backup/candidate.version'")"
  [[ "$revision" =~ ^[0-9a-f]{40}$ && "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 1
  if [[ "$mode" == resume ]]; then
    [[ "$(deploy_source_revision "$root")" == "$revision" ]] || { echo 'resume source differs from retained candidate' >&2; exit 1; }
  fi
fi
ssh "$ssh_target" sudo -n bash -s -- "$slot" "$remote_mode" "$staging" "$backup" "$revision" "$version" "$seal" \
  < "$root/scripts/deploy/lib/po-central-remote.sh"
printf 'collector %s %s: source=%s backup=%s staging=%s\n' "$slot" "$remote_mode" "$revision" "$backup" "$staging"
