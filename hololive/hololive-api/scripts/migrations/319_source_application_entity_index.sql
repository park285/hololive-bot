-- 코드 열 기준 UNIQUE index를 별도 autocommit 문장으로 만든다. 320이 구 index를, 321이 text 열을 제거한다.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_source_observation_application_entity
    ON public.source_observation_applications (observation_id, entity_kind_id, entity_key);
