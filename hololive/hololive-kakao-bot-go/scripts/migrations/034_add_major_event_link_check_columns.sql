-- 034: major_events 링크 신뢰성 추적 컬럼 추가
-- 목적: member news에서 실패/차단 링크를 제외하고, stale 링크를 주기적으로 재검증

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'major_events' AND column_name = 'link_status'
    ) THEN
        ALTER TABLE major_events ADD COLUMN link_status VARCHAR(20) DEFAULT 'unchecked';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'major_events' AND column_name = 'link_checked_at'
    ) THEN
        ALTER TABLE major_events ADD COLUMN link_checked_at TIMESTAMPTZ;
    END IF;
END $$;

UPDATE major_events
SET link_status = 'unchecked'
WHERE link_status IS NULL;

ALTER TABLE major_events
    ALTER COLUMN link_status SET DEFAULT 'unchecked',
    ALTER COLUMN link_status SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_major_events_link_status ON major_events(link_status);
CREATE INDEX IF NOT EXISTS idx_major_events_link_checked_at ON major_events(link_checked_at);

-- Rollback:
-- ALTER TABLE major_events DROP COLUMN IF EXISTS link_checked_at;
-- ALTER TABLE major_events DROP COLUMN IF EXISTS link_status;
-- DROP INDEX IF EXISTS idx_major_events_link_checked_at;
-- DROP INDEX IF EXISTS idx_major_events_link_status;
