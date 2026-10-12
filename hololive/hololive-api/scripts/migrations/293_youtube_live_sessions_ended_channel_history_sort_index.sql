-- 채널을 지정한 방송 이력 목록의 정렬·커서 순서다. 297이 같은 순서의 식 인덱스
-- idx_yls_ended_channel_sort_video를 지운다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_ended_channel_history_sort
    ON public.youtube_live_sessions (channel_id, history_sort_at DESC, video_id DESC)
    WHERE status = 'ENDED';
