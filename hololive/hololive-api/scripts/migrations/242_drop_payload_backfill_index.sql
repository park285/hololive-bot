-- Cutover transaction이 끝난 뒤 미처리 행 전용 임시 index만 제거합니다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_source_observations_payload_backfill;
