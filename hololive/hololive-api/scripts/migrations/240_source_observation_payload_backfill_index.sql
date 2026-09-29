-- 이미 처리한 PK 접두사를 매 배치 재탐색하지 않도록 미처리 행만 색인합니다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observations_payload_backfill
    ON public.source_observations (id) WHERE payload_id IS NULL;
