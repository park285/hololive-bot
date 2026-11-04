-- 스키마 마이그레이션이 아닌 명시적 관리자 소켓 작업이며 파일 배포만으로 운영 실행을 승인하지 않는다.
-- 수집기 네 풀 모두 최대값 7을 적용하고 갱신·종결 경합 검증을 통과한 뒤에만 실행한다.
-- 롤백용 이전 rolconnlimit를 기록하고 한도를 복구하기 전에 풀을 늘리지 않는다.
-- 모든 풀에 scripts/ci/postgres-capacity-policy.tsv의 역할 한도를 반영한다.
BEGIN;
ALTER ROLE hololive_runtime CONNECTION LIMIT 25;
ALTER ROLE hololive_scraper CONNECTION LIMIT 28;
ALTER ROLE hololive_migrator CONNECTION LIMIT 1;
-- postgres_exporter 0.20.x는 legacy 연결 1개를 유지하고 scrape마다 collector 연결 1개를 더 열고 닫는다.
-- 한도 1이면 매 scrape가 FATAL(too many connections for role)로 끝나고 pg_up만 남는다(2026-10-19~24 실측).
ALTER ROLE postgres_exporter CONNECTION LIMIT 2;
ALTER ROLE iris_db_nightly CONNECTION LIMIT 2;
COMMIT;
