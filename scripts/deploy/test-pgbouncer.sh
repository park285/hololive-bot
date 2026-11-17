#!/usr/bin/env bash
# 내부 network와 각 fixture의 private netns에서만 운영 주소·인증 계약을 재현한다.
set -euo pipefail
if [[ -n "${CONTAINER_HOST:-}${CONTAINER_CONNECTION:-}" ]] || [[ "$(podman info --format '{{.Host.Security.Rootless}}')" != true ]]; then
  echo 'PgBouncer product test requires local rootless Podman' >&2
  exit 2
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
pg_image="${POSTGRES_POLICY_TEST_IMAGE:-docker.io/library/postgres:18.7-alpine@sha256:b6972e1115a4ff8eba5d7f7d19184926d1e16d7d1136ae1e1a7ebf286da0f015}"
pool_image="${PGBOUNCER_TEST_IMAGE:?set PGBOUNCER_TEST_IMAGE to the locally built host-architecture image}"
case "$(uname -m)" in
  x86_64) architecture=amd64 ;;
  aarch64) architecture=arm64 ;;
  *) echo 'unsupported test architecture' >&2; exit 2 ;;
esac
for image in "$pg_image" "$pool_image"; do
  [[ "$(podman image inspect "$image" --format '{{.Architecture}}')" == "$architecture" ]] || {
    echo 'test image architecture differs from this host; build/select the matching image' >&2
    exit 2
  }
done
fixture="$(mktemp -d)"
id="holo-pgb-$(date +%s)-$$"
server="${id}-server"
pooler="${id}-pooler"
central="${id}-central"
socket="${id}-socket"
containers=()
networks=()
socket_created=false
cleanup() {
  local status=$? failed=false name
  trap - EXIT
  for name in "${containers[@]}"; do
    if podman container exists "$name"; then
      podman rm --force "$name" >/dev/null || failed=true
    fi
  done
  if [[ "$socket_created" == true ]]; then podman volume rm "$socket" >/dev/null || failed=true; fi
  for name in "${networks[@]}"; do podman network rm "$name" >/dev/null || failed=true; done
  rm -r -- "$fixture" || failed=true
  if [[ "$failed" == true ]]; then echo 'PgBouncer fixture cleanup incomplete' >&2; exit 1; fi
  exit "$status"
}
trap cleanup EXIT
(
  export GOWORK=off
  cd "$root/hololive/hololive-shared"
  CGO_ENABLED=0 go build -mod=readonly -trimpath -o "$fixture/probe" "$root/scripts/deploy/fixtures/pgbouncer/main.go"
  cd "$root/hololive/hololive-api"
  CGO_ENABLED=0 go build -mod=readonly -trimpath -o "$fixture/db-migrate" ./cmd/db-migrate
)
"$fixture/probe" prepare "$fixture"
# 키는 namespace 안의 서비스 UID만 읽는다. 저장소 bind에는 SELinux label을 변경하지 않는다.
podman unshare chown 10001:10001 "$fixture/pooler-tls/server.key" "$fixture/pooler-tls/backend-client.key"
printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=3\n' >"$fixture/target.env"
. "$root/scripts/deploy/lib/postgres-capacity.sh"
postgres_capacity_write_pgbouncer_config "$root/scripts/ci/postgres-capacity-policy.tsv" "$fixture/target.env" "$fixture/pgbouncer.ini"
podman network create --internal --subnet 172.23.0.0/16 "$central" >/dev/null
networks+=("$central")
podman volume create --driver local --opt type=tmpfs --opt device=tmpfs --opt o=size=16m,mode=3775 "$socket" >/dev/null
socket_created=true
containers+=("$server")
podman run -d --name "$server" --pull=never --platform="linux/$architecture" --security-opt label=disable \
  --network "$central:ip=172.23.255.253,alias=holo-postgres" --memory=1g --pids-limit=128 \
  --tmpfs /var/lib/postgresql:rw,size=512m \
  --mount "type=volume,src=$socket,dst=/var/run/postgresql" \
  --mount "type=bind,src=$fixture,dst=/fixture,readonly" \
  --mount "type=bind,src=$root/deploy/postgres/pg_hba.conf,dst=/policy/pg_hba.conf,readonly" \
  -e POSTGRES_USER=postgres_admin -e POSTGRES_DB=hololive -e POSTGRES_PASSWORD_FILE=/fixture/admin-password \
  -e PGDATA=/var/lib/postgresql/pgdata --entrypoint /bin/sh "$pg_image" -ec '
    cp /fixture/pg-tls/server.crt /var/lib/postgresql/server.crt
    cp /fixture/pg-tls/server.key /var/lib/postgresql/server.key
    cp /fixture/ca.crt /var/lib/postgresql/ca.crt
    chown postgres:postgres /var/lib/postgresql/server.crt /var/lib/postgresql/server.key
    chmod 0600 /var/lib/postgresql/server.key
    exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/var/lib/postgresql/server.crt \
      -c ssl_key_file=/var/lib/postgresql/server.key -c ssl_ca_file=/var/lib/postgresql/ca.crt \
      -c hba_file=/policy/pg_hba.conf -c max_connections=61 -c log_statement=none -c log_min_error_statement=panic
  ' >/dev/null
ready=false
for _ in $(seq 1 60); do
  if podman exec "$server" pg_isready -h 127.0.0.1 -U postgres_admin -d hololive >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
[[ "$ready" == true ]] || { podman logs "$server" >&2; echo 'PostgreSQL fixture startup failed' >&2; exit 1; }
podman exec "$server" psql -X -v ON_ERROR_STOP=1 -q -U postgres_admin -d hololive -f /fixture/setup.sql >"$fixture/setup.log" 2>&1 || {
  echo 'isolated role fixture setup failed' >&2; exit 1;
}
podman run --rm --pull=never --platform="linux/$architecture" --security-opt label=disable --network none \
  --mount "type=volume,src=$socket,dst=/var/run/postgresql" \
  --mount "type=bind,src=$fixture,dst=/fixture,readonly" \
  --env-file "$fixture/bootstrap.env" --entrypoint /fixture/db-migrate "$pg_image" >"$fixture/bootstrap.log" 2>&1 || {
    echo 'isolated application bootstrap/migrations failed' >&2; exit 1;
  }
podman exec "$server" psql -X -v ON_ERROR_STOP=1 -q -U postgres_admin -d hololive -f /fixture/records.sql
podman exec -i "$server" psql -X -v ON_ERROR_STOP=1 -q -U postgres_admin -d hololive <"$root/deploy/postgres/role-limits.sql"
client() {
  local network="$1" address="$2" mode="$3" argument="$4" name="${id}-client"
  local client_network="$network:ip=$address"
  if [[ "$mode" == auth-client ]]; then client_network="container:$pooler"; fi
  local -a fault_control=()
  if [[ "$mode" == behavior ]]; then
    fault_control=(--pid="container:$pooler" --cap-add=KILL)
  fi
  containers+=("$name")
  podman run --rm --name "$name" --pull=never --platform="linux/$architecture" --security-opt label=disable \
    --network "$client_network" --read-only --cap-drop=all "${fault_control[@]}" --security-opt no-new-privileges \
    --mount "type=bind,src=$fixture,dst=/fixture,readonly" \
    --mount "type=volume,src=$socket,dst=/var/run/postgresql,readonly" \
    --entrypoint /fixture/probe "$pg_image" "$mode" "$argument"
}
namespace_address() {
  podman run --rm --pull=never --platform="linux/$architecture" --security-opt label=disable \
    --network "container:$1" --read-only --cap-drop=all --cap-add=NET_ADMIN --security-opt no-new-privileges \
    --entrypoint ip "$pg_image" address add "$2/24" dev eth0
}
remote_client() {
  local address="$1" slot="$2" name="${id}-ap-client"
  containers+=("$name")
  podman run -d --name "$name" --pull=never --platform="linux/$architecture" --security-opt label=disable \
    --network "$central" --read-only --cap-drop=all --security-opt no-new-privileges \
    --mount "type=bind,src=$fixture,dst=/fixture,readonly" \
    --init --stop-signal SIGTERM --entrypoint sleep "$pg_image" 120 >/dev/null
  namespace_address "$name" "$address"
  podman exec "$name" /fixture/probe identity "$slot"
  podman rm --force "$name" >/dev/null
}
podman run --rm --pull=never --platform="linux/$architecture" --security-opt label=disable --network none \
  --mount "type=volume,src=$socket,dst=/var/run/postgresql,readonly" --entrypoint psql "$pg_image" \
  'host=/var/run/postgresql dbname=hololive user=hololive_migrator sslmode=disable' -X -v ON_ERROR_STOP=1 \
  -c 'CREATE TABLE public.private_migration_probe(id integer); DROP TABLE public.private_migration_probe;' >/dev/null
podman exec "$server" psql -X -v ON_ERROR_STOP=1 -q -U iris_db_nightly -d hololive -Atc 'SELECT 1' | grep -qx 1
containers+=("$pooler")
podman run -d --name "$pooler" --pull=never --platform="linux/$architecture" --security-opt label=disable \
  --network "$central:ip=172.23.255.254,alias=holo-pgbouncer" \
  --read-only --read-only-tmpfs=false --tmpfs /tmp:rw,size=16m,mode=1777,noexec,nosuid,nodev \
  --cap-drop=all --security-opt no-new-privileges --memory=128m --pids-limit=64 --ulimit nofile=1024:1024 \
  --mount "type=bind,src=$fixture/pgbouncer.ini,dst=/etc/pgbouncer/pgbouncer.ini,readonly" \
  --mount "type=bind,src=$root/deploy/pgbouncer/pg_hba.conf,dst=/etc/pgbouncer/pg_hba.conf,readonly" \
  --mount "type=bind,src=$root/deploy/pgbouncer/stats-users.txt,dst=/etc/pgbouncer/stats-users.txt,readonly" \
  --mount "type=bind,src=$fixture/pooler-tls,dst=/run/hololive-bot/pgbouncer,readonly" \
  --mount "type=bind,src=$fixture/ca.crt,dst=/run/hololive-bot/certs/postgres-ca.pem,readonly" \
  "$pool_image" >/dev/null
namespace_address "$pooler" 100.100.1.8
ready=false
for _ in $(seq 1 30); do
  if podman exec "$pooler" /usr/local/bin/pgbouncer-startup-health holo-pgbouncer holo-postgres >"$fixture/version" 2>"$fixture/health-error"; then ready=true; break; fi
  sleep 1
done
[[ "$ready" == true ]] || { cat "$fixture/health-error" >&2; echo 'PgBouncer startup health failed' >&2; exit 1; }
cat "$fixture/version"
client "$central" 172.23.255.254 auth-client central
client "$central" 172.23.10.9 wrong-auth-source central
(
  podman() {
    [[ "$1" == exec && "$2" == holo-pgbouncer ]] || return 2
    shift 2
    command podman exec "$pooler" "$@"
  }
  units="$root/deploy/quadlet/hololive-bot"
  postgres_capacity_assert_live "$root" "$fixture/target.env" "$units" >"$fixture/capacity-check.log" 2>&1
  printf 'ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n' >"$fixture/changed-target.env"
  postgres_capacity_write_pgbouncer_config "$root/scripts/ci/postgres-capacity-policy.tsv" \
    "$fixture/changed-target.env" "$fixture/pgbouncer.ini" >>"$fixture/capacity-check.log"
  if postgres_capacity_assert_live "$root" "$fixture/changed-target.env" "$units" >>"$fixture/capacity-check.log" 2>&1; then
    echo 'Changed host configuration was accepted while the pooler retained its old bind input' >&2
    exit 1
  fi
  postgres_capacity_assert_live "$root" "$fixture/target.env" "$units" >>"$fixture/capacity-check.log" 2>&1
  postgres_capacity_write_pgbouncer_config "$root/scripts/ci/postgres-capacity-policy.tsv" \
    "$fixture/target.env" "$fixture/pgbouncer.ini" >>"$fixture/capacity-check.log"
)
printf 'PASS live capacity guard rejects atomic host replacement until pooler recreation\n'
client "$central" 172.23.10.10 identity central
remote_client 100.100.1.6 a
remote_client 100.100.1.5 b
remote_client 100.100.1.3 d
client "$central" 172.23.10.10 behavior central
printf 'PASS: transaction pooling identity, TLS, auth rotation, prepared statements and reserved control; no production endpoints used\n'
