-- 모든 live consume은 현재 slot을 scheduled_for 동등 조건으로 다시 읽는다. 채널 GIN만으로는
-- 해당 채널의 무기한 보존 이력 전체를 heap에서 재검사하므로 slot 시각으로 먼저 좁힌다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_live_absence_slots_scheduled_for
    ON youtube_live_absence_slots (scheduled_for);
