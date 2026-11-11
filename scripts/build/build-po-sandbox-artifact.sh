#!/usr/bin/env bash
# kapu에서만 빌드·내보내기·검증한다. 같은 이미지로 Compose·Quadlet image와 native rootfs를 만든다.
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
# image tar의 이름은 적재하는 런타임이 찾는 이름이어야 한다. docker load는 docker.io/library/를 떼어 Seoul AP
# compose의 hololive-youtube-po-sandbox:prod로 등록한다. 중앙 podman load는 이름을 그대로 쓰며, Quadlet은
# 재시작할 때마다 Image= tag를 다시 읽으므로 :candidate로 적재하고 :prod는 cutover가 옮긴다.
# rootfs는 native AP용으로 image tar를 만들지 않는다.
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

# docker 형식은 HEALTHCHECK 메타데이터를 보존하고, docker-archive 저장 때 config를 변환하지 않아
# identity(config digest)가 save 전후로 같다.
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
  # systemd가 RootDirectory에 만드는 API 파일시스템 mountpoint를 검증 전에 포함한다.
  # 이미지에 없는 디렉터리를 기동 때 추가하면 배포 후 불변 rootfs 검사가 실패한다.
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
  # 이미지 tar는 Compose·Quadlet 전송에만 쓰며 릴리스 전에 정확한 이미지 식별자를 보존한다.
  podman save --format docker-archive --output "$output/image.tar" "$image"
  (cd "$output" && sha256sum image.tar > image.tar.sha256)
  python3 "$root/scripts/build/po-sandbox-manifest.py" image-ids "$output/image.tar" \
    "sha256:$(podman image inspect -f '{{.Id}}' "$image")" > "$output/image-id"
fi
printf '%s\n' "$revision" > "$output/revision"
printf '%s\n' "$arch" > "$output/architecture"
printf '%s\n' "$version" > "$output/version"
echo "issuer artifact: image=$image target=$target revision=$revision arch=$arch output=$output"
