#!/usr/bin/env bash
# kapu에서만 빌드·내보내기·검증한다. 같은 이미지로 Compose와 native rootfs를 만든다.
set -euo pipefail
[[ "$(hostname -s)" == kapu ]] || { echo 'issuer image build/export/verification is restricted to kapu' >&2; exit 1; }
root="$(cd "$(dirname "$0")/../.." && pwd)"
if [[ $# -ne 4 ]]; then
  echo "Usage: $0 amd64|arm64 FULL_SOURCE_SHA VERSION OUTPUT_DIR" >&2
  exit 2
fi
arch="$1"
revision="$2"
version="$3"
output="$4"
[[ "$arch" == amd64 || "$arch" == arm64 ]] || exit 2
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || { echo 'full SHA required' >&2; exit 2; }
[[ "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 2
image="hololive-youtube-po-sandbox:prod"
[[ ! -e "$output" ]] || { echo "issuer output already exists: $output" >&2; exit 1; }
mkdir -p "$output"
output="$(cd "$output" && pwd)"
working="$(mktemp -d)"
container=''
cleanup() {
  [[ -z "$container" ]] || docker rm "$container" >/dev/null
  rm -rf "$working"
}
trap cleanup EXIT

docker buildx build --platform "linux/$arch" --provenance=false --sbom=false --load \
  --tag "$image" \
  --file "$root/hololive/hololive-youtube-collector/Dockerfile.po-sandbox" \
  --build-arg "REVISION=$revision" --build-arg "VERSION=$version" "$root"
actual_revision="$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")"
actual_arch="$(docker image inspect -f '{{.Os}}/{{.Architecture}}' "$image")"
[[ "$actual_revision" == "$revision" && "$actual_arch" == "linux/$arch" ]] || {
  echo 'issuer image revision/architecture mismatch' >&2; exit 1;
}
container="$(docker create "$image")"
mkdir -p "$working/rootfs"
docker export "$container" --output "$output/rootfs.tar"
python3 "$root/scripts/build/po-sandbox-manifest.py" verify-socket-owner "$output/rootfs.tar"
tar -xf "$output/rootfs.tar" -C "$working/rootfs" --no-same-owner --same-permissions
python3 "$root/scripts/build/po-sandbox-manifest.py" create "$working/rootfs" "$output/rootfs-manifest.json" "$revision" "$arch"
python3 "$root/scripts/build/po-sandbox-manifest.py" verify "$working/rootfs" "$output/rootfs-manifest.json" "$revision" "$arch"
(cd "$output" && sha256sum rootfs.tar > rootfs.tar.sha256)
# 이미지 tar는 Compose 전송에만 쓰며 릴리스 전에 정확한 이미지 식별자를 보존한다.
docker save --output "$output/image.tar" "$image"
(cd "$output" && sha256sum image.tar > image.tar.sha256)
python3 "$root/scripts/build/po-sandbox-manifest.py" image-ids "$output/image.tar" \
  "$(docker image inspect -f '{{.Id}}' "$image")" > "$output/image-id"
printf '%s\n' "$revision" > "$output/revision"
printf '%s\n' "$arch" > "$output/architecture"
printf '%s\n' "$version" > "$output/version"
echo "issuer artifact: image=$image revision=$revision arch=$arch output=$output"
