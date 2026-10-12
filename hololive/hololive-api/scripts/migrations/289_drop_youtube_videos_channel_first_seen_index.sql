-- youtube_videos는 video_id나 (channel_id, is_short)로 찾고 first_seen_at 순서로 읽는 런타임 조회가 없다. 채널
-- 조건은 idx_yv_channel_is_short가 처리한다. 2026-10-11 운영 조회: idx_scan 4(마지막 2026-09-21, 수동 진단 조회로 본다).
DROP INDEX CONCURRENTLY IF EXISTS public.idx_yv_channel_first_seen;
