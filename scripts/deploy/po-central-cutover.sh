#!/usr/bin/env bash
# Isolated issuer + central collector c: kapu-only builds, verified image transfers, no remote builds.
set -Eeuo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$root/scripts/deploy/lib/source-revision.sh"
[[ "$(hostname -s)" == kapu ]] || { echo 'image build/export is restricted to kapu' >&2; exit 1; }
[[ $# -ge 1 && $# -le 2 ]] || { echo "Usage: $0 deploy | rollback|check /opt/hololive-bot/compose/backups/po-c-TIMESTAMP" >&2; exit 2; }
mode="$1"
[[ "$mode" == deploy || "$mode" == rollback || "$mode" == check ]] || exit 2
ssh_target="${PO_C_SSH_TARGET:?set PO_C_SSH_TARGET to the approved central SSH host}"
[[ "$ssh_target" =~ ^[A-Za-z0-9@._-]+$ ]] || exit 2
if [[ "$mode" == deploy ]]; then
  [[ $# -eq 1 && "${APPROVE_PO_C_DEPLOY:-}" == true ]] || { echo 'APPROVE_PO_C_DEPLOY=true required' >&2; exit 2; }
elif [[ "$mode" == rollback ]]; then
  [[ $# -eq 2 && "${APPROVE_PO_C_ROLLBACK:-}" == true ]] || { echo 'APPROVE_PO_C_ROLLBACK=true required' >&2; exit 2; }
else
  [[ $# -eq 2 ]] || exit 2
fi
# Gate immediately before the first live mutation; the source SHA itself is checked below.
gate_root="${PO_META_ROOT:-/home/kapu/work/iris-stack}"
if [[ "$mode" != check ]]; then
  (cd "$gate_root" && bash tools/checks/check-decision-catalog.sh plans gate PLN-20260927-hololive-egress-po-production --json) | \
    python3 -c 'import json,sys; g=json.load(sys.stdin); assert g["strict_validation"] == "passed" and g["gate_passed"] is True'
fi
version="$(cat "$root/hololive/hololive-api/VERSION")"
[[ "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 1
remote_root=/opt/hololive-bot/compose
if [[ "$mode" == deploy ]]; then
  revision="$(deploy_source_revision "$root")"
  change_id="$(date -u +%Y%m%dT%H%M%SZ)"
  backup="$remote_root/backups/po-c-$change_id"
  staging="$remote_root/po-staging-$change_id"
  local_stage="$(mktemp -d)"
  trap 'rm -rf "$local_stage"' EXIT
  "$root/scripts/build/build-po-sandbox-artifact.sh" arm64 "$revision" "$version" "$local_stage/issuer"
  docker buildx build --platform linux/arm64 --provenance=false --sbom=false --load \
    --tag hololive-youtube-collector:prod \
    --file "$root/hololive/hololive-youtube-collector/Dockerfile" \
    --build-arg "REVISION=$revision" --build-arg "VERSION=$version" "$root"
  [[ "$(docker image inspect -f '{{.Os}}/{{.Architecture}}' hololive-youtube-collector:prod)" == linux/arm64 ]]
  [[ "$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' hololive-youtube-collector:prod)" == "$revision" ]]
  collector_id="$(docker create hololive-youtube-collector:prod)"
  docker cp "$collector_id:/app/bin/youtube-collector" "$local_stage/collector-binary"
  docker cp "$collector_id:/app/manifest.json" "$local_stage/collector-manifest.json"
  docker rm "$collector_id" >/dev/null
  python3 - "$local_stage/collector-manifest.json" "$local_stage/collector-binary" "$revision" <<'PY'
import hashlib, json, sys
m=json.load(open(sys.argv[1])); assert m['source_revision'] == sys.argv[3] and m['go']['goarch'] == 'arm64'
assert hashlib.sha256(open(sys.argv[2], 'rb').read()).hexdigest() == m['files']['bin/youtube-collector']
PY
  docker save --output "$local_stage/collector-image.tar" hololive-youtube-collector:prod
  (cd "$local_stage" && sha256sum collector-image.tar > collector-image.tar.sha256)
  python3 "$root/scripts/build/po-sandbox-manifest.py" image-ids "$local_stage/collector-image.tar" \
    "$(docker image inspect -f '{{.Id}}' hololive-youtube-collector:prod)" > "$local_stage/collector-image-id"
  ssh "$ssh_target" "sudo -n install -d -o root -g root -m 0700 '$staging'"
  rsync -a --rsync-path='sudo -n rsync' \
    --files-from="$root/scripts/deploy/po-central-files.txt" "$root/" "$ssh_target:$staging/"
  rsync -a --rsync-path='sudo -n rsync' \
    "$local_stage/issuer/image.tar" "$local_stage/issuer/image.tar.sha256" \
    "$local_stage/issuer/image-id" "$local_stage/issuer/rootfs-manifest.json" "$local_stage/collector-image.tar" \
    "$local_stage/collector-image.tar.sha256" "$local_stage/collector-image-id" "$local_stage/collector-manifest.json" \
    "$ssh_target:$staging/"
  ssh "$ssh_target" "sudo -n chown -R root:root '$staging' && sudo -n chmod -R go-w '$staging'"
else
  backup="$2"
  [[ "$backup" =~ ^/opt/hololive-bot/compose/backups/po-c-[0-9]{8}T[0-9]{6}Z$ ]] || exit 2
  change_id="${backup##*/po-c-}"
  staging="$remote_root/po-staging-$change_id"
  revision="$(ssh "$ssh_target" "sudo -n cat '$backup/candidate.revision'")"
  version="$(ssh "$ssh_target" "sudo -n cat '$backup/candidate.version'")"
  [[ "$revision" =~ ^[0-9a-f]{40}$ && "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 1
fi
ssh "$ssh_target" sudo -n bash -s -- "$mode" "$staging" "$backup" "$revision" "$version" \
  < "$root/scripts/deploy/lib/po-central-remote.sh"
printf 'central %s: source=%s backup=%s staging=%s\n' "$mode" "$revision" "$backup" "$staging"
