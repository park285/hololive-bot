-- youtube_community_shorts_alarm_states를 detected_at 순서나 범위로 읽는 런타임 조회가 없다. 행은 PK와
-- idx_ycsas_kind_content로 찾는다. 2026-10-11 운영 조회: idx_scan 2(마지막 2026-09-21, 수동 진단 조회로 본다).
DROP INDEX CONCURRENTLY IF EXISTS public.idx_ycsas_detected_at;
