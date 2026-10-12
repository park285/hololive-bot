-- youtube_community_shorts_source_posts는 PK upsert만 하고 channel_id·detected_at으로 읽는 조회가 없다.
-- 2026-10-11 운영 조회: 2026-08-23 이후 idx_scan 0. 쓰기 비용만 남는다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_ycssp_channel_detected;
