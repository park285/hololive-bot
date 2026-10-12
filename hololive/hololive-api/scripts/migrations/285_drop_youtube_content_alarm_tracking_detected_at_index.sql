-- 관리 telemetry는 COALESCE(actual_published_at, detected_at)로 거르고 정렬하므로 detected_at 단독 인덱스를 쓸 수
-- 없다. 다른 런타임 조회는 (kind, content_id)나 PK로 찾는다. 2026-10-11 운영 조회: 2026-08-23 이후 idx_scan 0.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_ycat_detected_at;
