-- kind별 id 순서 조회는 운영 경로에 없고 kind 범위 조회는 (observation_kind, received_at, id)가 담당한다.
DROP INDEX CONCURRENTLY IF EXISTS idx_source_observations_kind_id;
