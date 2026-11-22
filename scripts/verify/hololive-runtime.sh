#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
Usage: scripts/verify/hololive-runtime.sh run [--worker-binary FILE] [--request METHOD PATH STATUS BODY]...
       scripts/verify/hololive-runtime.sh --help
Build the real API, alarm worker and H3 caller. Use a new private PostgreSQL,
Valkey and Iris readiness fixture. Check identity/auth/empty state, drive a
LIVE subscription and a schedule-poster memo, and remove all owned resources.
--worker-binary substitutes a negative control. The last line is the proof path.
Optional requests target only this instance's /api/holo/, /internal/alarm/ or
/l/ routes after both journeys. Use an empty string for an absent body.
USAGE
}

if [[ ${1-} == --help && $# == 1 ]]; then usage; exit 0; fi
if [[ ${1-} != run ]]; then usage >&2; exit 2; fi
shift
binary=
if [[ $# -ge 2 && $1 == --worker-binary ]]; then binary=$(realpath -e "$2"); shift 2; fi
extra=()
while (($#)); do
    if [[ $# -lt 5 || $1 != --request || ! $2 =~ ^(GET|HEAD|POST|PUT|PATCH|DELETE)$ || ! $3 =~ ^/(api/holo|internal/alarm|l)/ || ! $4 =~ ^[1-5][0-9][0-9]$ ]]; then
        usage >&2
        exit 2
    fi
    extra+=("$2" "$3" "$4" "$5")
    shift 5
done
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
umask 077
evidence=$(mktemp -d /tmp/c3-hololive-proof.XXXXXXXX)
fixture=$(mktemp -d /tmp/c3-hololive-data.XXXXXXXX)
name=c3-hololive-${fixture##*.}
network=
db=
cache=
worker=
api=
iris=
podman() { command podman --remote=false "$@"; }
cleanup() {
    local rc=$?
    trap - EXIT INT TERM
    set +e
    if [[ -n $api ]]; then
        podman logs "$api" >"$evidence/api.log" 2>&1 || rc=1
        podman rm -fv "$api" >>"$evidence/cleanup" 2>&1 || rc=1
    fi
    if [[ -n $iris ]]; then
        podman logs "$iris" >"$evidence/iris.log" 2>&1 || rc=1
        podman rm -fv "$iris" >>"$evidence/cleanup" 2>&1 || rc=1
    fi
    if [[ -n $worker ]]; then
        podman logs "$worker" >"$evidence/worker.log" 2>&1 || rc=1
        podman rm -fv "$worker" >>"$evidence/cleanup" 2>&1 || rc=1
    fi
    if [[ -n $cache ]]; then podman rm -fv "$cache" >>"$evidence/cleanup" 2>&1 || rc=1; fi
    if [[ -n $db ]]; then podman rm -fv "$db" >>"$evidence/cleanup" 2>&1 || rc=1; fi
    if [[ -n $network ]]; then podman network rm "$network" >>"$evidence/cleanup" 2>&1 || rc=1; fi
    rm -rf -- "$fixture" || rc=1
    printf '%s\n' "$rc" >"$evidence/exit-code"
    printf '%s\n' "$evidence"
    exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
exec 3>&2
exec 2> >(tee "$evidence/stderr" >&3)
postgres=docker.io/library/postgres@sha256:8b0ff4cdf221f0c53d7fcda681d3b461be725c492cd881c5ec9c0ad25023f188
valkey=localhost/iris-stack-valkey:9.1.3-zlib1.4-r0
revision=$(git rev-parse HEAD)
if [[ -z $binary ]]; then
    CGO_ENABLED=0 go build -p 2 -ldflags "-X main.Version=$revision" -o "$fixture/worker" ./hololive/hololive-alarm-worker/cmd/alarm-worker
else
    cp -- "$binary" "$fixture/worker"
fi
CGO_ENABLED=0 go build -p 2 -ldflags "-X main.Version=$revision" -o "$fixture/api" ./hololive/hololive-api/cmd/hololive-api
sha256sum "$fixture/api" >"$evidence/api.sha256"
CGO_ENABLED=0 go build -p 2 -o "$fixture/client" ./scripts/verify/client.go
sha256sum "$fixture/worker" >"$evidence/worker.sha256"
go version -m "$fixture/worker" >"$evidence/build-info"
cp -r hololive/hololive-api/scripts/migrations "$fixture/migrations"
cp hololive/hololive-alarm-worker/internal/config/testdata/stack-worker-profile-alarm-worker.json "$fixture/profile.json"
cp hololive/hololive-api/internal/config/configtest/testdata/stack-worker-profile-api.json "$fixture/api-profile.json"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 -subj /CN=c3-hololive -addext subjectAltName=IP:127.0.0.1 -keyout "$fixture/key.pem" -out "$fixture/cert.pem" 2>"$evidence/cert-generation"
key=$(openssl rand -hex 24)
pgpass=$(openssl rand -hex 24)
network=$(podman network create --internal --driver bridge "$name")
db=$(podman create --pull=never --network "$network" --name "$name-pg" --tmpfs /var/lib/postgresql:rw -v "$fixture:/fixture:ro,z" -e POSTGRES_USER=dbtest -e POSTGRES_DB=dbtest -e "POSTGRES_PASSWORD=$pgpass" -e 'POSTGRES_INITDB_ARGS=--locale-provider=builtin --builtin-locale=C.UTF-8 --encoding=UTF8 --data-checksums' "$postgres" postgres -c listen_addresses=127.0.0.1 -c fsync=off)
podman start "$db" >"$evidence/started"
cache=$(podman create --pull=never --network "container:$db" --name "$name-cache" "$valkey" valkey-server --bind 127.0.0.1 --save '' --appendonly no)
podman start "$cache" >>"$evidence/started"
printf '%s\n' "revision=$revision" "instance=$name" "network=$network" "db=$db" "valkey=$cache" >"$evidence/identity"
sql() { podman exec "$db" psql -XAt -v ON_ERROR_STOP=1 -U dbtest -d dbtest -c "$1"; }
deadline=$((SECONDS + 60))
until podman exec "$db" pg_isready --quiet -h 127.0.0.1 -U dbtest -d dbtest; do
    if (( SECONDS >= deadline )); then printf 'PostgreSQL startup timed out\n' >&2; exit 1; fi
    sleep 0.2
done
sql "CREATE ROLE hololive_alarm_worker LOGIN PASSWORD '$pgpass'" >"$evidence/role"
sql "CREATE ROLE hololive_runtime LOGIN PASSWORD '$pgpass'" >>"$evidence/role"
sql 'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hololive_runtime; ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE ON SEQUENCES TO hololive_runtime' >>"$evidence/role"
podman exec -e HOLOLIVE_MIGRATIONS_DIR=/fixture/migrations "$db" /fixture/client migrate >"$evidence/migrate" 2>&1
sql 'SELECT current_database(),current_user,count(*) FROM hololive_dbtest_internal.schema_migrations' >"$evidence/database"
runtime=(create --pull=never --network "container:$db" -v "$fixture:/fixture:ro,z" -w /tmp
    -e APP_ENV=development -e GOMAXPROCS=2 -e GIN_MODE=release
    -e HOLOLIVE_H3_CERT_FILE=/fixture/cert.pem -e HOLOLIVE_H3_KEY_FILE=/fixture/key.pem
    -e "API_SECRET_KEY=$key" -e POSTGRES_HOST=127.0.0.1 -e POSTGRES_SSLMODE=disable
    -e POSTGRES_DB=dbtest -e "POSTGRES_PASSWORD=$pgpass" -e CACHE_HOST=127.0.0.1
    -e IRIS_BASE_URL=https://127.0.0.1:3443 -e IRIS_BOT_TOKEN=disposable
    -e IRIS_H3_CA_CERT_FILE=/fixture/cert.pem -e IRIS_BASE_URL_ALLOWED_HOSTS=127.0.0.1
    -e HOLODEX_BASE_URL=https://127.0.0.1:3443 -e HOLODEX_API_KEY=disposable)
iris=$(podman create --pull=never --network "container:$db" -v "$fixture:/fixture:ro,z" --name "$name-iris" --entrypoint /fixture/client "$postgres" iris)
podman start "$iris" >>"$evidence/started"
worker=$(podman "${runtime[@]}" --name "$name-worker" --entrypoint /fixture/worker \
    -e STACK_WORKER_PROFILE_FILE=/fixture/profile.json -e POSTGRES_USER=hololive_alarm_worker \
    -e NOTIFICATION_SCHEDULER_ROLE=off -e HOLOLIVE_H3_ADDR=127.0.0.1:30007 "$postgres")
podman start "$worker" >>"$evidence/started"
api=$(podman "${runtime[@]}" --name "$name-api" --entrypoint /fixture/api \
    -e STACK_WORKER_PROFILE_FILE=/fixture/api-profile.json -e POSTGRES_USER=hololive_runtime \
    -e KAKAO_ROOMS=930003 -e IRIS_WEBHOOK_TOKEN=c3-webhook -e PHOTO_SYNC_ENABLED=false \
    -e HOLOLIVE_H3_ADDR=127.0.0.1:30001 -e HOLOLIVE_ADMIN_API_H3_ADDR=127.0.0.1:30006 \
    -e HOLOLIVE_INTERNAL_H3_SERVER_NAME=127.0.0.1 \
    -e HOLOLIVE_INTERNAL_H3_CA_CERT_FILE=/fixture/cert.pem -e ALARM_INTERNAL_URL=https://127.0.0.1:30007 "$postgres")
podman start "$api" >>"$evidence/started"
printf 'worker=%s\napi=%s\niris=%s\n' "$worker" "$api" "$iris" >>"$evidence/identity"
request() {
    local label=$1 method=$2 path=$3 expected=$4 auth=$5 body=$6 rc=0
    printf '%s %s\n%s\nexpected HTTP %s\n' "$method" "$path" "$body" "$expected" >"$evidence/$label.request"
    podman exec -e "C3_KEY=$auth" "$db" /fixture/client "$method" "$path" "$expected" "$body" >"$evidence/$label.json" 2>"$evidence/$label.status" || rc=$?
    printf '%s\n' "$rc" >"$evidence/$label.exit"
    return "$rc"
}
await_feature() {
    local label=$1 path=$2 owner=$3 deadline=$((SECONDS + 40))
    until request "$label" GET "$path" 200 "$key" ''; do
        if grep -Eq '^[1-5][0-9][0-9] ' "$evidence/$label.status" || [[ $(podman inspect -f '{{.State.Running}}' "$owner") != true ]] || (( SECONDS >= deadline )); then
            cat "$evidence/$label.status" >&2
            printf 'Feature not ready: %s\n' "$path" >&2
            exit 1
        fi
        sleep 0.2
    done
}
check() {
    printf '%s\n' "$2" >"$evidence/$1.expected"
    if ! jq -e "$2" "$evidence/$1.json" >/dev/null; then
        printf '%s: expected %s, got %s\n' "$1" "$2" "$(cat "$evidence/$1.json")" >&2
        exit 1
    fi
}
await_feature ready /internal/alarm/count "$worker"
check ready '.success == true and .data.count == 0'
request doctor-auth GET /internal/alarm/count 401 '' ''
request before GET /internal/alarm/room/c3-room/view 200 "$key" ''
check before '.success == true and (.data | length) == 0'
memo=/api/holo/schedule-posters/memo
await_feature memo-before "$memo" "$api"
check memo-before '.revision == "1" and .body == ""'
request memo-auth GET "$memo" 401 '' ''
request doctor-version GET /api/holo/stats 200 "$key" ''
check doctor-version ".status == \"ok\" and .version == \"$revision\" and .alarms == 0"
request add POST /internal/alarm/add 200 "$key" '{"room_id":"c3-room","channel_id":"c3-channel","user_id":"c3-user","room_name":"검증방","alarm_types":["LIVE"]}'
check add '.success == true and .data.added == true'
request view GET /internal/alarm/room/c3-room/view 200 "$key" ''
check view '.success == true and (.data | length) == 1 and .data[0].ChannelID == "c3-channel" and .data[0].AlarmTypes == ["LIVE"]'
sql "SELECT room_id,channel_id,user_id,alarm_types FROM public.alarms ORDER BY id" >"$evidence/persisted"
printf '%s\n' 'c3-room|c3-channel|c3-user|{LIVE}' >"$evidence/persisted.expected"
diff -u "$evidence/persisted.expected" "$evidence/persisted"
request remove POST /internal/alarm/remove 200 "$key" '{"room_id":"c3-room","channel_id":"c3-channel","alarm_types":["LIVE"]}'
check remove '.success == true and .data.removed == true'
request after GET /internal/alarm/count 200 "$key" ''
check after '.success == true and .data.count == 0'
sql 'SELECT count(*) FROM public.alarms' >"$evidence/removed"
grep -Fxq 0 "$evidence/removed"
request memo-save POST "$memo" 200 "$key" '{"expectedRevision":"1","body":"검증 메모"}'
check memo-save '.revision == "2"'
request memo-conflict POST "$memo" 409 "$key" '{"expectedRevision":"1","body":"오래된 수정"}'
request memo-after GET "$memo" 200 "$key" ''
check memo-after '.revision == "2" and .body == "검증 메모" and (.updatedAt | type) == "string"'
sql 'SELECT revision,body FROM public.schedule_poster_translation_memo' >"$evidence/memo-persisted"
grep -Fxq '2|검증 메모' "$evidence/memo-persisted"
for ((i=0; i<${#extra[@]}; i+=4)); do
    request "extra-$((i/4+1))" "${extra[i]}" "${extra[i+1]}" "${extra[i+2]}" "$key" "${extra[i+3]}"
done
