-- 현재 방송 조회는 장기 absence 이력이 아니라 최근 LIVE 범위의 증거만 읽는다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_live_absence_slots_live_time
    ON youtube_live_absence_slots (effective_at DESC)
    WHERE (coverage -> 'filters' -> 'statuses') ? 'LIVE';
