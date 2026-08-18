-- 182_postgres_idle_transaction_timeout.sql
-- 목적: 열린 transaction 안에서 장시간 idle 상태가 된 세션이 VACUUM horizon을
-- 붙잡아 dead tuple 회수와 XID freeze를 지연시키는 운영 위험을 제한한다.
--
-- idle 상태가 아닌 active statement에는 적용되지 않으며, 일반 idle pool connection도
-- 종료하지 않는다. ALTER DATABASE 기본값은 새로 시작한 session부터 적용되므로 배포 후
-- 애플리케이션 connection pool을 순차 재기동해야 한다.
--
-- 롤백:
-- DO $rollback$
-- BEGIN
--     EXECUTE pg_catalog.format(
--         'ALTER DATABASE %I RESET idle_in_transaction_session_timeout',
--         pg_catalog.current_database()
--     );
-- END
-- $rollback$;

DO $migration$
BEGIN
    EXECUTE pg_catalog.format(
        'ALTER DATABASE %I SET idle_in_transaction_session_timeout = %L',
        pg_catalog.current_database(),
        '5min'
    );
END
$migration$;
