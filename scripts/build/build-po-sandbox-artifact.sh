#!/usr/bin/env bash
set -euo pipefail
[[ "$(hostname -s)" == kapu ]] || { echo 'issuer image build/export/verification is restricted to kapu' >&2; exit 1; }
root="$(cd "$(dirname "$0")/../.." && pwd)"
if [[ $# -ne 5 ]]; then
  echo "Usage: $0 amd64|arm64 FULL_SOURCE_SHA VERSION OUTPUT_DIR docker|podman|rootfs" >&2
  exit 2
fi
arch="$1"
revision="$2"
version="$3"
output="$4"
target="$5"
[[ "$arch" == amd64 || "$arch" == arm64 ]] || exit 2
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || { echo 'full SHA required' >&2; exit 2; }
[[ "$version" =~ ^[A-Za-z0-9._-]+$ ]] || exit 2
case "$target" in
  docker) image=docker.io/library/hololive-youtube-po-sandbox:prod ;;
  podman|rootfs) image=localhost/hololive-youtube-po-sandbox:candidate ;;
  *) exit 2 ;;
esac
[[ ! -e "$output" ]] || { echo "issuer output already exists: $output" >&2; exit 1; }
mkdir -p "$output"
output="$(cd "$output" && pwd)"
working="$(mktemp -d)"
container=''
cleanup() {
  [[ -z "$container" ]] || podman rm "$container" >/dev/null
  rm -rf "$working"
}
trap cleanup EXIT

podman build --platform "linux/$arch" --format docker \
  --tag "$image" \
  --file "$root/hololive/hololive-youtube-collector/Dockerfile.po-sandbox" \
  --build-arg "REVISION=$revision" --label "org.opencontainers.image.revision=$revision" \
  --build-arg "VERSION=$version" "$root"
actual_revision="$(podman image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")"
actual_arch="$(podman image inspect -f '{{.Os}}/{{.Architecture}}' "$image")"
[[ "$actual_revision" == "$revision" && "$actual_arch" == "linux/$arch" ]] || {
  echo 'issuer image revision/architecture mismatch' >&2; exit 1;
}
container="$(podman create "$image")"
mkdir -p "$working/rootfs"
podman export "$container" --output "$output/rootfs.tar"
python3 "$root/scripts/build/po-sandbox-manifest.py" verify-socket-owner "$output/rootfs.tar"
tar -xf "$output/rootfs.tar" -C "$working/rootfs" --no-same-owner --same-permissions
if [[ "$target" == rootfs ]]; then
  mountpoints=()
  for name in dev proc sys; do
    if [[ -e "$working/rootfs/$name" || -L "$working/rootfs/$name" ]]; then
      [[ -d "$working/rootfs/$name" && ! -L "$working/rootfs/$name" ]] || {
        echo "native API filesystem mountpoint must be a directory: $name" >&2
        exit 1
      }
    else
      install -d -m0555 "$working/rootfs/$name"
      mountpoints+=("$name")
    fi
  done
  if (( ${#mountpoints[@]} )); then
    tar --append --file "$output/rootfs.tar" --directory "$working/rootfs" \
      --numeric-owner --owner=0 --group=0 "${mountpoints[@]}"
  fi
fi
python3 "$root/scripts/build/po-sandbox-manifest.py" create "$working/rootfs" "$output/rootfs-manifest.json" "$revision" "$arch"
python3 "$root/scripts/build/po-sandbox-manifest.py" verify "$working/rootfs" "$output/rootfs-manifest.json" "$revision" "$arch"
(cd "$output" && sha256sum rootfs.tar > rootfs.tar.sha256)
if [[ "$target" != rootfs ]]; then
  podman save --format docker-archive --output "$output/image.tar" "$image"
  (cd "$output" && sha256sum image.tar > image.tar.sha256)
  python3 "$root/scripts/build/po-sandbox-manifest.py" image-ids "$output/image.tar" \
    "sha256:$(podman image inspect -f '{{.Id}}' "$image")" > "$output/image-id"
fi
printf '%s\n' "$revision" > "$output/revision"
printf '%s\n' "$arch" > "$output/architecture"
printf '%s\n' "$version" > "$output/version"
echo "issuer artifact: image=$image target=$target revision=$revision arch=$arch output=$output"
