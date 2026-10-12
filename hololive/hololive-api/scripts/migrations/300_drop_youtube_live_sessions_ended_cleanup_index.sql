-- 종료 세션을 ended_at 범위나 순서로 읽는 런타임 조회가 없다. 2026-10-11 운영 조회: idx_scan 10(마지막 2026-09-26).
-- 코드에 이 술어를 쓰는 조회가 없으므로 수동·진단 조회로 본다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_yls_ended_cleanup;
