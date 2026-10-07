-- 183의 DB 기본값을 다시 선언한다. 183은 2026-08-20 운영에 적용됐지만 2026-10-08 서울 운영 pg_db_role_setting에
-- hololive 행이 없다. 2026-08-23 PG 18.6 전환은 pg_dump -Fc → 새 볼륨 initdb → pg_restore였고 복원 플래그 기록이
-- 없으므로, --create 없이 복원해 설정이 빠졌다는 것은 추론이다. DB 수준 설정은 평문 pg_dump --create·pg_dumpall
-- 출력, archive의 pg_restore --create, 물리 복사로는 옮겨지지만 --create 없는 pg_restore로는 빠진다.
-- ledger에는 183이 남아 러너가 다시 실행하지 않고, 적용된 파일은 checksum 원장 때문에 고칠 수 없으므로 같은
-- DO 블록을 새 번호로 둔다. 같은 값의 재설정은 멱등이다.
--
-- 이 기본값은 새로 시작한 session부터 적용되므로 배포 후 애플리케이션 connection pool을 순차 재기동해야 한다.
-- 전제: database owner 또는 superuser만 실행할 수 있다(2026-10-08 서울 운영 owner는 hololive_migrator).
DO $migration$
BEGIN
    EXECUTE pg_catalog.format(
        'ALTER DATABASE %I SET idle_in_transaction_session_timeout = %L',
        pg_catalog.current_database(),
        '5min'
    );
END
$migration$;
