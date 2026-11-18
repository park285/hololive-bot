BEGIN;
ALTER ROLE hololive_runtime CONNECTION LIMIT 17;
ALTER ROLE hololive_alarm_worker CONNECTION LIMIT 8;
ALTER ROLE hololive_scraper NOLOGIN CONNECTION LIMIT 0;
ALTER ROLE hololive_collector_a CONNECTION LIMIT 7;
ALTER ROLE hololive_collector_b CONNECTION LIMIT 7;
ALTER ROLE hololive_collector_c CONNECTION LIMIT 7;
ALTER ROLE hololive_collector_d CONNECTION LIMIT 7;
ALTER ROLE hololive_migrator CONNECTION LIMIT 1;
ALTER ROLE hololive_pgbouncer_auth CONNECTION LIMIT 1;
-- postgres_exporter 0.20.x는 legacy 연결 1개를 유지하고 scrape마다 collector 연결 1개를 더 열고 닫는다.
-- 한도 1이면 매 scrape가 FATAL(too many connections for role)로 끝나고 pg_up만 남는다.
ALTER ROLE postgres_exporter CONNECTION LIMIT 2;
ALTER ROLE iris_db_nightly CONNECTION LIMIT 2;
COMMIT;
