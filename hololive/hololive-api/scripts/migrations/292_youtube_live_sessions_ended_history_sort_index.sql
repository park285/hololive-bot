-- 채널을 지정하지 않은 방송 이력 목록의 정렬·커서 순서다. 291의 생성 열을 key로 써 last_seen_at 갱신이 key를
-- 바꾸지 않는다. 296이 같은 순서의 식 인덱스 idx_yls_ended_sort_video를 지운다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_ended_history_sort
    ON public.youtube_live_sessions (history_sort_at DESC, video_id DESC)
    WHERE status = 'ENDED';
