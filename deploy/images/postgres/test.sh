#!/bin/sh
set -eu

if [ "$(id -u)" = 0 ]; then
  [ "$(gosu 999:999 id -u)" = 999 ]
  [ "$(gosu 999:999 id -g)" = 999 ]
else
  [ "$(id -u)" = 999 ]
fi

start_server() {
  docker-entrypoint.sh postgres -c listen_addresses= \
    -c pg_stat_kcache.track=top -c pg_stat_kcache.track_planning=off \
    -c pg_wait_sampling.profile_pid=off -c pg_wait_sampling.profile_queries=top \
    -c pg_wait_sampling.sample_cpu=off -c pg_wait_sampling.profile_period=20 \
    -c pg_wait_sampling.history_period=100 -c pg_wait_sampling.history_size=20000 "$@" &
  server=$!
  trap 'kill -TERM "$server" 2>/dev/null || true; wait "$server" || true' EXIT
  attempts=0
  while :; do
    kill -0 "$server"
    # postmaster PID를 비교하여 initdb의 임시 서버와 QEMU의 프로세스 이름에 의존하지 않습니다.
    postmaster_pid=
    if [ -f "$PGDATA/postmaster.pid" ]; then
      read -r postmaster_pid < "$PGDATA/postmaster.pid"
    fi
    if [ "$postmaster_pid" = "$server" ] && pg_isready -h /var/run/postgresql -U postgres >/dev/null 2>&1; then
      break
    fi
    attempts=$((attempts + 1))
    [ "$attempts" -lt 30 ]
    sleep 1
  done
}

stop_server() {
  kill -TERM "$server"
  wait "$server"
  trap - EXIT
}

start_observability() {
  start_server -c shared_preload_libraries=pg_stat_statements,pg_stat_kcache,pg_wait_sampling \
    -c compute_query_id=on
}

# 이미지 설치만으로 활성화되지 않으며 preload 누락 시 SQL 전체가 거부되어야 합니다.
start_server
# 운영 public 스키마와 같은 경계로 검증합니다. 모니터는 public USAGE를 받지 않습니다.
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 \
  -c 'REVOKE USAGE ON SCHEMA public FROM PUBLIC;'
if psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 \
  -f /usr/local/share/hololive/enable-observability.sql; then
  printf 'activation unexpectedly succeeded without preload\n' >&2
  exit 1
fi
[ "$(psql -X -h /var/run/postgresql -U postgres -Atqc "SELECT count(*) FROM pg_extension WHERE extname IN ('pg_stat_kcache','pg_wait_sampling')")" = 0 ]
stop_server
start_observability
[ "$(psql -h /var/run/postgresql -U postgres -Atqc 'SELECT current_user')" = postgres ]
[ "$(psql -h /var/run/postgresql -U postgres -Atqc 'SHOW server_version_num')" = 180006 ]
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 \
  -f /usr/local/share/hololive/enable-observability.sql
# 재적용이 객체나 권한을 망가뜨리지 않아야 합니다.
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 \
  -f /usr/local/share/hololive/enable-observability.sql
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 <<'SQL'
SELECT sum(octet_length(md5(value::text))) FROM generate_series(1, 200000) AS numbers(value);
SELECT pg_sleep(0.2);
DO $verify$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM hololive_observability.pg_stat_kcache() AS kernel
        JOIN public.pg_stat_statements AS statement
          ON statement.queryid = kernel.queryid AND statement.dbid = kernel.dbid
         AND statement.userid = kernel.userid AND statement.toplevel = kernel.top
        WHERE statement.query LIKE 'SELECT sum(octet_length(md5%'
          AND kernel.exec_user_time + kernel.exec_system_time > 0
    ) THEN
        RAISE EXCEPTION 'query CPU counters were not collected';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM hololive_observability.pg_wait_sampling_profile
        WHERE pid = 0 AND event = 'PgSleep' AND queryid <> 0 AND count > 0
    ) THEN
        RAISE EXCEPTION 'query wait samples were not collected';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'extension_test_app') THEN
        CREATE ROLE extension_test_app;
        CREATE ROLE extension_test_monitor;
        GRANT pg_read_all_stats TO extension_test_monitor;
    END IF;
    IF has_function_privilege('extension_test_app', 'hololive_observability.pg_stat_kcache()', 'EXECUTE')
       OR has_table_privilege('extension_test_app', 'hololive_observability.pg_wait_sampling_profile', 'SELECT')
       OR has_function_privilege('extension_test_monitor', 'hololive_observability.pg_stat_kcache_reset()', 'EXECUTE')
       OR has_function_privilege('extension_test_monitor', 'hololive_observability.pg_wait_sampling_reset_profile()', 'EXECUTE')
       OR has_schema_privilege('extension_test_monitor','public','USAGE')
       OR has_schema_privilege('extension_test_monitor','hololive_observability','CREATE')
       OR has_schema_privilege('extension_test_app','hololive_observability','USAGE') THEN
        RAISE EXCEPTION 'observability privileges exceed the intended boundary';
    END IF;
END
$verify$;
SET ROLE extension_test_monitor;
DO $monitor$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM hololive_observability.pg_stat_kcache())
       OR NOT EXISTS (SELECT 1 FROM hololive_observability.pg_wait_sampling_profile) THEN
        RAISE EXCEPTION 'monitor role cannot read collected statistics';
    END IF;
END
$monitor$;
RESET ROLE;
SET ROLE extension_test_app;
DO $app$
BEGIN
    BEGIN
        PERFORM * FROM hololive_observability.pg_stat_kcache();
        RAISE EXCEPTION 'app unexpectedly read kernel statistics';
    EXCEPTION WHEN insufficient_privilege THEN
        NULL;
    END;
    BEGIN
        PERFORM * FROM hololive_observability.pg_wait_sampling_profile;
        RAISE EXCEPTION 'app unexpectedly read wait statistics';
    EXCEPTION WHEN insufficient_privilege THEN
        NULL;
    END;
END
$app$;
RESET ROLE;
SQL

# 의도적인 서로 다른 세션의 행 잠금 대기를 수집합니다.
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 -c \
  'CREATE TABLE extension_lock_test (id integer PRIMARY KEY, value integer NOT NULL); INSERT INTO extension_lock_test VALUES (1, 0);'
PGAPPNAME=extension_lock_holder psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 -c \
  'BEGIN; UPDATE extension_lock_test SET value=value+1 WHERE id=1; SELECT pg_sleep(3); COMMIT;' >/dev/null &
holder=$!
attempts=0
until [ "$(psql -X -h /var/run/postgresql -U postgres -Atqc "SELECT count(*) FROM pg_stat_activity WHERE application_name='extension_lock_holder' AND wait_event='PgSleep'")" = 1 ]; do
  kill -0 "$holder"
  attempts=$((attempts + 1))
  [ "$attempts" -lt 20 ]
  sleep 0.1
done
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 -c \
  'SET statement_timeout = 5000; UPDATE extension_lock_test SET value=value+1 WHERE id=1;'
wait "$holder"
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 <<'SQL'
DO $lock$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM hololive_observability.pg_wait_sampling_profile
                   WHERE event_type='Lock' AND event='transactionid' AND queryid<>0 AND count>0) THEN
        RAISE EXCEPTION 'row lock wait samples were not collected';
    END IF;
END
$lock$;
DROP TABLE extension_lock_test;
SQL

# 정상 종료·재시작에서 kcache 누적값은 복원되고 wait 표본은 새로 시작합니다.
stop_server
start_observability
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 <<'SQL'
DO $restart$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM hololive_observability.pg_stat_kcache() WHERE exec_user_time+exec_system_time>0) THEN
        RAISE EXCEPTION 'kernel counters were not restored after clean restart';
    END IF;
    IF EXISTS (SELECT 1 FROM hololive_observability.pg_wait_sampling_profile WHERE event='PgSleep') THEN
        RAISE EXCEPTION 'old wait profile unexpectedly survived restart';
    END IF;
END
$restart$;
SQL
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 \
  -c 'SET ROLE extension_test_monitor' -f /usr/local/share/hololive/read-observability.sql
psql -X -h /var/run/postgresql -U postgres -v ON_ERROR_STOP=1 \
  -f /usr/local/share/hololive/disable-observability.sql
[ "$(psql -X -h /var/run/postgresql -U postgres -Atqc "SELECT count(*) FROM pg_extension WHERE extname IN ('pg_stat_kcache','pg_wait_sampling')")" = 0 ]
stop_server
start_server -c shared_preload_libraries=pg_stat_statements
[ "$(psql -X -h /var/run/postgresql -U postgres -Atqc 'SELECT count(*)>=0 FROM public.pg_stat_statements')" = t ]
stop_server
printf 'PostgreSQL startup, statistics, row waits, privileges, restart and rollback passed (initial uid=%s)\n' "$(id -u)"
