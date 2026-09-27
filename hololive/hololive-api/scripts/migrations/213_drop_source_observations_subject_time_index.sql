-- shorts claim이 활성 queue 집합으로 범위를 좁힌 뒤 운영 조회가 이 인덱스를 쓰지 않는다.
-- 매 관측 insert의 쓰기 증폭과 보존 용량만 남으므로 제거한다.
DROP INDEX CONCURRENTLY IF EXISTS idx_source_observations_subject_time;
