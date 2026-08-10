-- epoch-2 squash checkpoint: 이 파일이 적용된 DB만 001_schema_epoch2_baseline.sql을
-- 적용 완료로 인정한다. dbtest 재생에는 schema_migrations가 없으므로 to_regclass로 가드한다
-- (prod runner는 적용 전 항상 ledger를 Ensure하므로 실제 배포 경로에서는 반드시 기록된다).
DO $$
BEGIN
    IF to_regclass('schema_migrations') IS NOT NULL THEN
        INSERT INTO schema_migrations (filename)
        VALUES ('001_schema_epoch2_baseline.sql')
        ON CONFLICT (filename) DO NOTHING;
    END IF;
END
$$;
