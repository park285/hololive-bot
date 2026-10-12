-- 보존 삭제(repository_retention_delete_queue_0076_76.sql)가 만료된 PROCESSED 행을 처리 시각 범위로 찾는다. 인덱스가
-- 없으면 PROCESSED 이력 전체를 읽고 updated_at으로 정렬해, 운영(2026-10-12, 약 22만 행)에서 평균 166 ms·최대 9.2초였다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_queue_processed_retention
    ON public.source_observation_queue (processed_at, observation_id)
    WHERE status = 'PROCESSED';
