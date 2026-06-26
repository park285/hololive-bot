-- 027: 월간 알림 발송 추적용 notified_month 컬럼 추가
-- Idempotent: IF NOT EXISTS 사용

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'major_events' AND column_name = 'notified_month'
    ) THEN
        ALTER TABLE major_events ADD COLUMN notified_month VARCHAR(10);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_major_events_notified_month ON major_events(notified_month);

-- Rollback:
-- ALTER TABLE major_events DROP COLUMN IF EXISTS notified_month;
-- DROP INDEX IF EXISTS idx_major_events_notified_month;
