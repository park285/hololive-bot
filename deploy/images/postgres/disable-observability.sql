-- 승인된 복구에서 새 이미지가 아직 로드된 동안 먼저 실행합니다. 기존 pg_stat_statements는 보존합니다.
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';
-- 외부 의존 객체가 있으면 중단합니다. CASCADE로 앱/모니터링 객체를 지우지 않습니다.
DROP VIEW IF EXISTS hololive_observability.statement_resources RESTRICT;
DROP VIEW IF EXISTS hololive_observability.statement_info RESTRICT;
DROP EXTENSION IF EXISTS pg_wait_sampling RESTRICT;
DROP EXTENSION IF EXISTS pg_stat_kcache RESTRICT;
DROP SCHEMA IF EXISTS hololive_observability RESTRICT;
COMMIT;
