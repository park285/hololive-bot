-- shared_preload_libraries=pg_stat_statements 선행이 전제조건(compose command).
-- CREATE EXTENSION은 superuser 필요라 hololive_migrator(NOSUPERUSER) 불가 → init-db(admin)에 둔다.
-- 기존 운영 볼륨에는 init-db가 재실행되지 않으므로 1회 docker exec로 동일 문을 실행한다.
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
