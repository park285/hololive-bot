-- 보존 삭제는 (observation_kind, received_at, id) 인덱스를 사용하며 kind 없는 received_at 조회는 없다.
DROP INDEX CONCURRENTLY IF EXISTS idx_source_observations_received;
