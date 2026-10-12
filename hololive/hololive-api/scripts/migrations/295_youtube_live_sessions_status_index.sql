-- 상태별 세션 조회는 status 조건만 쓰고 last_seen_at으로 정렬하지 않는다. 299가 (status, last_seen_at DESC)
-- 인덱스를 지운다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_status
    ON public.youtube_live_sessions (status);
