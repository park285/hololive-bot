#!/usr/bin/env bash
# 일회용 PG18.6 제품 검사. 포트 공개·운영 환경·운영 데이터를 사용하지 않는다.
set -euo pipefail
if [[ -n "${DOCKER_HOST:-}${DOCKER_CONTEXT:-}${DOCKER_CONFIG:-}" ]] || [[ "$(docker context show)" != default ]]; then
  echo 'policy test requires the local default Docker context without overrides' >&2
  exit 2
fi
[[ "$(docker context inspect default --format '{{.Endpoints.docker.Host}}')" == unix:///var/run/docker.sock ]] || {
  echo 'policy test refuses a nonlocal Docker endpoint' >&2; exit 2;
}
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
image="${POSTGRES_POLICY_TEST_IMAGE:-hololive-postgres:prod}"
fixture="$(mktemp -d)"
id="holo-policy-$(date +%s)-$$"
server="${id}-server"
network="${id}-net"
socket="${id}-socket"
cleanup() {
  docker rm -f "$server" >/dev/null 2>&1 || true
  docker volume rm "$socket" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf "$fixture"
}
trap cleanup EXIT
# 이미 빌드된 고정 PostgreSQL 이미지만 재사용하며 내려받거나 빌드하지 않는다.
docker image inspect "$image" >/dev/null
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=holo-postgres \
  -addext subjectAltName=DNS:holo-postgres \
  -keyout "$fixture/server.key" -out "$fixture/server.crt" >/dev/null 2>&1
chmod 0755 "$fixture"
chmod 0644 "$fixture/server.crt"
# 서버 진입점이 합성 테스트 파일을 전용 데이터 tmpfs로 복사한다.
chmod 0644 "$fixture/server.key"
docker network create --internal --subnet 172.31.249.0/24 "$network" >/dev/null
docker volume create --driver local --opt type=tmpfs --opt device=tmpfs \
  --opt o=size=16m,mode=3775,uid=999,gid=999 "$socket" >/dev/null
docker run -d --name "$server" --pull never --network "$network" --network-alias holo-postgres \
  --tmpfs /var/lib/postgresql:rw,size=512m \
  --mount "type=volume,src=$socket,dst=/var/run/postgresql" \
  --mount "type=bind,src=$fixture,dst=/fixture,readonly" \
  --mount "type=bind,src=$root/deploy/compose/postgres/pg_hba.conf,dst=/policy/pg_hba.conf,readonly" \
  -e POSTGRES_USER=postgres_admin -e POSTGRES_PASSWORD=fixture -e POSTGRES_DB=hololive \
  -e PGDATA=/var/lib/postgresql/pgdata \
  --entrypoint /bin/sh "$image" -ec '
    cp /fixture/server.crt /var/lib/postgresql/server.crt
    cp /fixture/server.key /var/lib/postgresql/server.key
    chown postgres:postgres /var/lib/postgresql/server.*
    chmod 0600 /var/lib/postgresql/server.key
    exec docker-entrypoint.sh postgres -c ssl=on \
      -c ssl_cert_file=/var/lib/postgresql/server.crt \
      -c ssl_key_file=/var/lib/postgresql/server.key \
      -c hba_file=/policy/pg_hba.conf -c max_connections=60
  ' >/dev/null
for _ in $(seq 1 60); do
  if docker exec "$server" pg_isready -h 127.0.0.1 -U postgres_admin -d hololive >/dev/null 2>&1; then break; fi
  sleep 1
done
docker exec "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 -Atc \
  "SELECT current_setting('server_version_num') = '180006'" | tr -d '\r' | grep -qx t
docker exec -i "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
CREATE ROLE hololive_migrator LOGIN PASSWORD 'fixture';
CREATE ROLE hololive_runtime LOGIN PASSWORD 'fixture';
CREATE ROLE hololive_scraper LOGIN PASSWORD 'fixture';
CREATE ROLE postgres_exporter LOGIN PASSWORD 'fixture';
CREATE ROLE iris_db_nightly LOGIN CONNECTION LIMIT 2;
CREATE ROLE hololive_replicator LOGIN REPLICATION PASSWORD 'fixture';
GRANT CREATE ON SCHEMA public TO hololive_migrator;
SQL
docker exec -i "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 \
  <"$root/deploy/compose/postgres/role-limits.sql" >/dev/null
docker exec "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) = 0 FROM pg_hba_file_rules WHERE error IS NOT NULL" | grep -qx t
docker exec "$server" psql -X -U postgres_admin -d hololive -v ON_ERROR_STOP=1 -Atc \
  "SELECT count(*) = 5 AND bool_and(r.rolconnlimit = expected.maximum) FROM (VALUES ('hololive_runtime',25),('hololive_scraper',28),('hololive_migrator',1),('postgres_exporter',1),('iris_db_nightly',2)) expected(name,maximum) JOIN pg_roles r ON r.rolname=expected.name" | grep -qx t
client() {
  local role="$1" mode="$2" database="${3:-hololive}"
  docker run --rm --pull never --network "$network" \
    --mount "type=bind,src=$fixture/server.crt,dst=/ca.pem,readonly" \
    -e PGPASSWORD=fixture -e PGCONNECT_TIMEOUT=3 --entrypoint psql "$image" \
    "host=holo-postgres dbname=$database user=$role sslmode=$mode sslrootcert=/ca.pem" \
    -X -v ON_ERROR_STOP=1 -Atc 'SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()'
}
for role in hololive_runtime hololive_scraper postgres_exporter; do
  [[ "$(client "$role" verify-full)" == t ]]
  if client "$role" disable >"$fixture/rejected" 2>&1; then
    echo "plaintext TCP unexpectedly accepted: $role" >&2; exit 1
  fi
done
for role in postgres_admin hololive_migrator iris_db_nightly hololive_replicator; do
  if client "$role" verify-full >"$fixture/rejected" 2>&1; then
    echo "privileged TCP unexpectedly accepted: $role" >&2; exit 1
  fi
done
[[ "$(client postgres_exporter verify-full postgres)" == t ]]
# 마이그레이션은 네트워크 없이 읽기 전용 소켓 마운트로 실행 가능해야 한다.
docker run --rm --pull never --network none \
  --mount "type=volume,src=$socket,dst=/var/run/postgresql,readonly" \
  --entrypoint psql "$image" \
  'host=/var/run/postgresql dbname=hololive user=hololive_migrator sslmode=disable' \
  -X -v ON_ERROR_STOP=1 -c 'CREATE TABLE public.socket_migration_proof(id integer); DROP TABLE public.socket_migration_proof;' >/dev/null
docker exec "$server" psql -X -U iris_db_nightly -d hololive -v ON_ERROR_STOP=1 -Atc 'SELECT 1' | grep -qx 1
for role in postgres_admin hololive_migrator hololive_replicator; do
  if docker run --rm --pull never --network "$network" \
    --mount "type=bind,src=$fixture/server.crt,dst=/ca.pem,readonly" \
    -e PGPASSWORD=fixture -e PGCONNECT_TIMEOUT=3 --entrypoint psql "$image" \
    "host=holo-postgres user=$role replication=true sslmode=verify-full sslrootcert=/ca.pem" \
    -X -c IDENTIFY_SYSTEM >"$fixture/rejected" 2>&1; then
    echo "unapproved replication TCP unexpectedly accepted: $role" >&2; exit 1
  fi
done
printf 'PASS: PG18.6 isolated migration socket, runtime/exporter TLS, nightly socket and privileged TCP rejection\n'
