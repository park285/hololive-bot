#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
image="localhost/hololive-postgres-test:ci-$$"
cleanup() {
  podman image rm "$image" >/dev/null 2>&1 || true
}
trap cleanup EXIT

podman build --platform linux/amd64 --build-arg REVISION=test --tag "$image" "$root_dir/deploy/images/postgres"

for user in 0:0 999:999; do
  # SELinux host에서 저장소 파일을 relabel하지 않고 읽도록 일회용 검사 container만 label을 끈다.
  podman run --rm --platform linux/amd64 --security-opt label=disable --network none --read-only --memory 256m --pids-limit 128 --user "$user" \
    --tmpfs /var/lib/postgresql:rw,mode=0755,U \
    --tmpfs /var/run/postgresql:rw,mode=0755,U \
    --tmpfs /tmp:rw,mode=1777 \
    --mount "type=bind,src=$root_dir/deploy/images/postgres/test.sh,dst=/test.sh,readonly" \
    -e PGDATA=/var/lib/postgresql/testdata -e POSTGRES_HOST_AUTH_METHOD=trust \
    "$image" sh /test.sh
done
