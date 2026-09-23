#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
image="hololive-postgres-test:ci-$$"
cleanup() {
  docker image rm --no-prune "$image" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker buildx build --platform linux/amd64 --target test --output type=cacheonly \
  "$root_dir/deploy/images/deunhealth"
docker buildx build --platform linux/amd64 --build-arg REVISION=test \
  --provenance=false --sbom=false --load --tag "$image" "$root_dir/deploy/images/postgres"

for user in 0:0 999:999; do
  docker run --rm --platform linux/amd64 --network none --read-only --memory 256m --pids-limit 128 --user "$user" \
    --tmpfs /var/lib/postgresql:rw,uid=999,gid=999,mode=0755 \
    --tmpfs /var/run/postgresql:rw,uid=999,gid=999,mode=0755 \
    --tmpfs /tmp:rw,mode=1777 \
    --mount "type=bind,src=$root_dir/deploy/images/postgres/test.sh,dst=/test.sh,readonly" \
    -e PGDATA=/var/lib/postgresql/testdata -e POSTGRES_HOST_AUTH_METHOD=trust \
    "$image" sh /test.sh
done
