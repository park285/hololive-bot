-- 역할·데이터베이스·확장·스키마 bootstrap보다 먼저 실행해야 한다.
-- 검증에 실패하면 애플리케이션 소유 DB 객체를 만들지 않는다.
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
    RAISE EXCEPTION 'data_checksums must be on for a new PostgreSQL 18 cluster';
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
END;
$pg18_contract$;
