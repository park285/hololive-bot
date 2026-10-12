-- 채널별 세션 조회는 channel_id 조건만 쓰고 last_seen_at으로 정렬하지 않는다. last_seen_at을 빼 upsert 갱신이 이
-- 인덱스 key를 바꾸지 않게 한다. 298이 (channel_id, last_seen_at DESC) 인덱스를 지운다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_channel
    ON public.youtube_live_sessions (channel_id);
