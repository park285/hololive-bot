-- hololive-bot용 PostgreSQL 18.6+ runtime 계약 검사다.
--
-- 기존 volume에서는 bootstrap administrator 또는 pg_aios를 읽을 수 있는 역할
-- (superuser 또는 pg_read_all_stats)로 실행한다. 이 스크립트는 read-only이며
-- 배포 계약이 어긋나면 observability snapshot을 출력하기 전에 실패한다.
\set ON_ERROR_STOP on
\pset pager off

SELECT
  current_database() AS database_name,
  current_setting('server_version') AS server_version,
  current_setting('server_version_num')::integer AS server_version_num,
  (SELECT datlocprovider FROM pg_database WHERE datname = current_database()) AS locale_provider,
  (SELECT datlocale FROM pg_database WHERE datname = current_database()) AS builtin_locale,
  (SELECT datcollversion FROM pg_database WHERE datname = current_database()) AS collation_version,
  current_setting('data_directory') AS data_directory,
  current_setting('data_checksums') AS data_checksums,
  current_setting('io_method') AS io_method,
  current_setting('io_workers')::integer AS io_workers,
  current_setting('effective_io_concurrency')::integer AS effective_io_concurrency,
  current_setting('maintenance_io_concurrency')::integer AS maintenance_io_concurrency,
  current_setting('track_io_timing') AS track_io_timing,
  current_setting('track_wal_io_timing') AS track_wal_io_timing,
  current_setting('compute_query_id') AS compute_query_id,
  current_setting('shared_preload_libraries') AS shared_preload_libraries;

DO $pg18_contract$
DECLARE
  server_num integer := current_setting('server_version_num')::integer;
BEGIN
  IF server_num < 180006 OR server_num >= 190000 THEN
    RAISE EXCEPTION 'expected PostgreSQL 18.6 or newer within major 18, got %', current_setting('server_version');
  END IF;
  -- libc provider는 컨테이너 base image가 glibc↔musl로 바뀌면 정렬 순서가 달라져 기존 인덱스를 조용히 무효화한다.
  IF (SELECT datlocprovider FROM pg_database WHERE datname = current_database()) <> 'b' THEN
    RAISE EXCEPTION 'locale provider must be builtin, got %',
      (SELECT datlocprovider FROM pg_database WHERE datname = current_database());
  END IF;
  IF (SELECT datlocale FROM pg_database WHERE datname = current_database()) IS DISTINCT FROM 'C.UTF-8' THEN
    RAISE EXCEPTION 'builtin locale must be C.UTF-8, got %',
      coalesce((SELECT datlocale FROM pg_database WHERE datname = current_database()), '(none)');
  END IF;
  IF current_setting('data_checksums') <> 'on' THEN
    RAISE EXCEPTION 'data_checksums is off; enable it only with an approved offline pg_checksums procedure';
  END IF;
  IF current_setting('data_directory') <> '/var/lib/postgresql/pgdata' THEN
    RAISE EXCEPTION 'unexpected data_directory: %', current_setting('data_directory');
  END IF;
  IF current_setting('io_method') <> 'worker' THEN
    RAISE EXCEPTION 'io_method must be worker, got %', current_setting('io_method');
  END IF;
  IF current_setting('track_io_timing') <> 'on' OR current_setting('track_wal_io_timing') <> 'on' THEN
    RAISE EXCEPTION 'I/O timing collection must remain enabled';
  END IF;
  IF current_setting('compute_query_id') <> 'on' THEN
    RAISE EXCEPTION 'compute_query_id must be on';
  END IF;
  IF position('pg_stat_statements' IN current_setting('shared_preload_libraries')) = 0 THEN
    RAISE EXCEPTION 'pg_stat_statements must be preloaded';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements') THEN
    RAISE EXCEPTION 'pg_stat_statements extension is missing from database %', current_database();
  END IF;
END;
$pg18_contract$;

SELECT
  extension.extname,
  extension.extversion,
  owner.rolname AS extension_owner
FROM pg_extension AS extension
JOIN pg_roles AS owner ON owner.oid = extension.extowner
WHERE extension.extname = 'pg_stat_statements';

SELECT
  backend_type,
  object,
  context,
  reads,
  read_bytes,
  read_time,
  writes,
  write_bytes,
  write_time,
  fsyncs,
  fsync_time
FROM pg_stat_io
ORDER BY backend_type, object, context;

SELECT
  state,
  operation,
  target,
  count(io_id) AS handles,
  COALESCE(sum(length), 0) AS bytes
FROM pg_aios
GROUP BY state, operation, target
ORDER BY state, operation, target;
