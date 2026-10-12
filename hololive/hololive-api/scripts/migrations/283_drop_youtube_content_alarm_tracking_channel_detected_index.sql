-- youtube_content_alarm_tracking은 (kind, content_id)나 PK (kind, canonical_content_id)로만 찾고, channel_id와
-- detected_at 순서로 읽는 런타임 조회가 없다. 2026-10-11 운영 조회: 2026-08-23 이후 idx_scan 0. 쓰기 비용만 남는다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_ycat_channel_detected;
