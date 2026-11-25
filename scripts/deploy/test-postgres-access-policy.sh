#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${CONTAINER_HOST:-}${CONTAINER_CONNECTION:-}" ]] || [[ "$(podman info --format '{{.Host.Security.Rootless}}')" != true ]]; then
  echo 'policy test requires local rootless Podman without connection overrides' >&2
  exit 2
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
image="${POSTGRES_POLICY_TEST_IMAGE:-docker.io/library/postgres:19.0-alpine}"
fixture="$(mktemp -d)"
id="holo-policy-$(date +%s)-$$"
server="${id}-server"
network="${id}-net"
socket="${id}-socket"
cleanup() {
  podman rm -f "$server" >/dev/null 2>&1 || true
  podman volume rm "$socket" >/dev/null 2>&1 || true
  podman network rm "$network" >/dev/null 2>&1 || true
  podman unshare rm -rf "$fixture"
}
trap cleanup EXIT
podman image inspect "$image" >/dev/null
mkdir -m 0700 "$fixture/server"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=holo-postgres \
  -addext subjectAltName=DNS:holo-postgres \
  -keyout "$fixture/server/server.key" -out "$fixture/server/server.crt" >/dev/null 2>&1
chmod 0644 "$fixture/server/server.crt"
chmod 0600 "$fixture/server/server.key"
install -m 0644 "$root/deploy/postgres/pg_hba.conf" "$fixture/pg_hba.conf"
podman unshare chown -R 999:999 "$fixture/server"
podman network create --internal --subnet 172.31.249.0/24 "$network" >/dev/null
podman volume create --driver local --opt type=tmpfs --opt device=tmpfs \
  --opt o=size=16m,mode=3775,uid=999,gid=999 "$socket" >/dev/null
podman run -d --name "$server" --pull never --user 999:999 --security-opt label=disable --network "$network" --network-alias holo-postgres \
  --tmpfs /var/lib/postgresql:rw,size=512m,mode=0700,U \
  --mount "type=volume,src=$socket,dst=/var/run/postgresql" \
  --mount "type=bind,src=$fixture/server,dst=/fixture,readonly" \
  --mount "type=bind,src=$fixture/pg_hba.conf,dst=/policy/pg_hba.conf,readonly" \
  -e POSTGRES_USER=postgres_admin -e POSTGRES_PASSWORD=fixture -e POSTGRES_DB=hololive \
  -e PGDATA=/var/lib/postgresql/pgdata \
  --entrypoint /bin/sh "$image" -ec '
    cp /fixture/server.crt /var/lib/postgresql/server.crt
    cp /fixture/server.key /var/lib/postgresql/server.key
    chmod 0600 /var/lib/postgresql/server.key
    exec docker-entrypoint.sh postgres -c ssl=on \
      -c ssl_cert_file=/var/lib/postgresql/server.crt \
      -c ssl_key_file=/var/lib/postgresql/server.key \
      -c ssl_ca_file=/var/lib/postgresql/server.crt \
      -c hba_file=/policy/pg_hba.conf -c max_connections=60
  ' >/dev/null
for _ in $(seq 1 60); do
  if podman exec "$server" pg_isready -h 127.0.0.1 -U postgres_admin -d hololive >/dev/null 2>&1; then break; fi
  sleep 1
done
podman exec "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 -Atc \
  "SELECT current_setting('server_version_num') = '190000'" | tr -d '\r' | grep -qx t
podman exec -i "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
CREATE ROLE hololive_migrator LOGIN PASSWORD 'fixture';
CREATE ROLE hololive_runtime LOGIN PASSWORD 'fixture';
CREATE ROLE hololive_alarm_worker LOGIN PASSWORD 'fixture';
CREATE ROLE hololive_scraper NOLOGIN;
CREATE ROLE hololive_collector_a LOGIN PASSWORD 'fixture';
CREATE ROLE hololive_collector_b LOGIN PASSWORD 'fixture';
CREATE ROLE hololive_collector_c LOGIN PASSWORD 'fixture';
CREATE ROLE hololive_collector_d LOGIN PASSWORD 'fixture';
CREATE ROLE postgres_exporter LOGIN PASSWORD 'fixture';
CREATE ROLE iris_db_nightly LOGIN CONNECTION LIMIT 2;
CREATE ROLE hololive_replicator LOGIN REPLICATION PASSWORD 'fixture';
GRANT CREATE ON SCHEMA public TO hololive_migrator;
GRANT hololive_scraper TO hololive_collector_a, hololive_collector_b, hololive_collector_c, hololive_collector_d;
SQL
podman exec -i "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 \
  <"$root/deploy/postgres/role-limits.sql" >/dev/null
podman exec "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) = 0 FROM pg_hba_file_rules WHERE error IS NOT NULL" | grep -qx t
podman exec "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) = 10 AND bool_and(r.rolconnlimit = expected.maximum) AND bool_and(r.rolcanlogin = expected.can_login) FROM (VALUES ('hololive_runtime',17,true),('hololive_alarm_worker',8,true),('hololive_scraper',0,false),('hololive_collector_a',7,true),('hololive_collector_b',7,true),('hololive_collector_c',7,true),('hololive_collector_d',7,true),('hololive_migrator',1,true),('postgres_exporter',2,true),('iris_db_nightly',2,true)) expected(name,maximum,can_login) JOIN pg_roles r ON r.rolname=expected.name" | grep -qx t
client() {
  local role="$1" mode="$2" database="${3:-hololive}"
  podman run --rm --pull never --security-opt label=disable --network "$network" \
    --mount "type=bind,src=$fixture/server/server.crt,dst=/ca.pem,readonly" \
    -e PGPASSWORD=fixture -e PGCONNECT_TIMEOUT=3 --entrypoint psql "$image" \
    "host=holo-postgres dbname=$database user=$role sslmode=$mode sslrootcert=/ca.pem" \
    -X -v ON_ERROR_STOP=1 -Atc 'SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()'
}
role=postgres_exporter
[[ "$(client "$role" verify-full)" == t ]]
if client "$role" disable >"$fixture/rejected" 2>&1; then
  echo "plaintext TCP unexpectedly accepted: $role" >&2; exit 1
fi
for role in hololive_runtime hololive_alarm_worker hololive_collector_c; do
  [[ "$(client "$role" verify-full)" == t ]]
  if client "$role" disable >"$fixture/rejected" 2>&1; then
    echo "plaintext TCP unexpectedly accepted: $role" >&2; exit 1
  fi
done
for role in hololive_collector_a hololive_collector_b hololive_collector_d postgres_admin hololive_migrator hololive_scraper iris_db_nightly hololive_replicator; do
  for mode in verify-full disable; do
    if client "$role" "$mode" >"$fixture/rejected" 2>&1; then
      echo "direct TCP unexpectedly accepted: $role ($mode)" >&2; exit 1
    fi
  done
done
[[ "$(client postgres_exporter verify-full postgres)" == t ]]
podman run --rm --pull never --security-opt label=disable --network none \
  --mount "type=volume,src=$socket,dst=/var/run/postgresql,readonly" \
  --entrypoint psql "$image" \
  'host=/var/run/postgresql dbname=hololive user=hololive_migrator sslmode=disable' \
  -X -v ON_ERROR_STOP=1 -c 'CREATE TABLE public.socket_migration_proof(id integer); DROP TABLE public.socket_migration_proof;' >/dev/null
podman exec "$server" psql -X -U iris_db_nightly -d hololive -v ON_ERROR_STOP=1 -Atc 'SELECT 1' | grep -qx 1
for role in postgres_admin hololive_migrator hololive_replicator; do
  if podman run --rm --pull never --security-opt label=disable --network "$network" \
    --mount "type=bind,src=$fixture/server/server.crt,dst=/ca.pem,readonly" \
    -e PGPASSWORD=fixture -e PGCONNECT_TIMEOUT=3 --entrypoint psql "$image" \
    "host=holo-postgres user=$role replication=true sslmode=verify-full sslrootcert=/ca.pem" \
    -X -c IDENTIFY_SYSTEM >"$fixture/rejected" 2>&1; then
    echo "unapproved replication TCP unexpectedly accepted: $role" >&2; exit 1
  fi
done
printf 'PASS: PG19.0 isolated migration/nightly sockets, exporter and bridge writer TLS, AP-only and privileged TCP rejection\n'
