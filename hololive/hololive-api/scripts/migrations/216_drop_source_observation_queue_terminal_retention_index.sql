-- 종단 queue 보존 삭제는 processed_at/dead_lettered_at 조건과 updated_at 정렬을 함께 쓰므로
-- (status, updated_at) 부분 인덱스를 선택하지 않는다. 사용되지 않는 쓰기 증폭만 제거한다.
DROP INDEX CONCURRENTLY IF EXISTS idx_source_observation_queue_terminal_retention;
