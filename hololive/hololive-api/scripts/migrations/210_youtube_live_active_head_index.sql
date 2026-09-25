-- 상태 지표가 종료된 reconciliation 이력 전체를 30초마다 훑지 않게 합니다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_live_reconciliation_heads_active_video
    ON youtube_live_reconciliation_heads (video_id)
    WHERE status IN ('LIVE', 'UPCOMING');
