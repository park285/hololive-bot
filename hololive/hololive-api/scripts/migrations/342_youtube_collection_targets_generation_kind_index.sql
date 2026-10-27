-- collector 후보 조회는 runner마다 1초 주기(운영 하루 약 200만 회)로 실행된다. 인자가 바뀌지 않는데도
-- custom plan 추정 비용이 generic plan보다 항상 낮아 plan cache가 매 호출 다시 계획했다(운영 계획 0.5~0.7ms,
-- 실행 0.6ms). PK는 (generation, subject, kind) 순서라 generic plan에서 종류 배열을 skip scan으로 비싸게
-- 추정해 targets 전체 parallel seq scan(운영 60ms)을 골랐다. 세대·종류 순서 인덱스로 generic plan도
-- CURRENT 세대의 해당 종류만 읽게 한다. generic plan 고정은 343이 맡는다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_collection_targets_generation_kind
    ON public.youtube_collection_targets (projection_generation, observation_kind, subject_key);
