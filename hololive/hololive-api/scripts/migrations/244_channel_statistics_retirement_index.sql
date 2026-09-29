-- 234의 kind 정리와 contract 삭제 FK 확인이 큰 application 이력을 반복해서 스캔하지 않게 합니다.
-- manifest에서 234보다 먼저 실행하고, 이행 완료 후 243으로 제거합니다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_applications_contract_retirement
    ON public.source_observation_applications (observation_kind, provider);
