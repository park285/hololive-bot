-- 보존 삭제(repository_retention_delete_queue_0076_76.sql)의 DEAD_LETTER 분기를 302와 같이 dead-letter 시각 범위로 찾는다.
-- 두 분기가 각각 부분 인덱스를 써야 OR 조건 전체가 bitmap OR로 만료 행만 읽는다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_queue_dead_letter_retention
    ON public.source_observation_queue (dead_lettered_at, observation_id)
    WHERE status = 'DEAD_LETTER';
