-- youtube_content_alarm_tracking의 delivery_status는 갱신 대상일 뿐 조회 조건이 아니다. 행은 (kind, content_id)나
-- PK로만 찾는다. 2026-10-11 운영 조회: 2026-08-23 이후 idx_scan 0. 쓰기 비용만 남는다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_ycat_delivery_status;
