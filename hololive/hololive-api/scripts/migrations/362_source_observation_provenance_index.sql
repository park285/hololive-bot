-- 366의 provenance 정리가 쓰는 인덱스다. 사전 행을 지우면 FK(ON DELETE RESTRICT) 검사와 참조 확인이
-- source_observations를 provenance_id로 찾는데, 이 인덱스가 없으면 사전 행 하나마다 원본 약 180만 행을 전체 스캔한다.
-- 2026-11-02 운영: provenance 47,959행 중 원본이 참조하는 행은 17,708이고 하루 1,924행씩 늘었다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observations_provenance_id
    ON public.source_observations (provenance_id);
