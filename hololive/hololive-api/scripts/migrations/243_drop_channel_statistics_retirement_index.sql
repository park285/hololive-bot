-- 통계 contract 제거가 끝난 뒤 임시 참조 인덱스를 남겨 정상 적재 비용을 늘리지 않습니다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_source_observation_applications_contract_retirement;
