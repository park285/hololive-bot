-- 승인된 cutover 전용이다. API/schema 준비, 기존 관측 queue drain, collector a/b/c/d
-- quiescence와 검증한 bundle 준비를 확인한 뒤 owning operator가 적용한다.
-- 일반 migration manifest에 넣지 않으며 generation 2/1 관측의 의미를 변경하지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';
DO $cutover$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.source_observations observation
        JOIN public.source_observation_queue queue ON queue.observation_id = observation.id
        WHERE observation.provider = 'youtubejs'
          AND observation.observation_kind IN ('live_snapshot', 'video_live_check')
          AND queue.status IN ('PENDING', 'PROCESSING')
    ) THEN
        RAISE EXCEPTION 'live lifecycle cutover requires drained snapshot/video check queue';
    END IF;
    IF (SELECT count(observation_kind) FROM public.observation_contract_generations
        WHERE provider = 'youtubejs'
          AND ((observation_kind = 'live_snapshot' AND current_schema_version = 1 AND current_generation = 2)
            OR (observation_kind = 'video_live_check' AND current_schema_version = 1 AND current_generation = 1))) <> 2 THEN
        RAISE EXCEPTION 'live lifecycle cutover prior contract changed';
    END IF;
END
$cutover$;
UPDATE public.observation_contract_generations
SET current_generation = 3, updated_by = 'live-lifecycle-cutover', updated_at = now()
WHERE provider = 'youtubejs' AND observation_kind = 'live_snapshot'
  AND current_schema_version = 1 AND current_generation = 2;
UPDATE public.observation_contract_generations
SET current_schema_version = 2, current_generation = 2, updated_by = 'live-lifecycle-cutover', updated_at = now()
WHERE provider = 'youtubejs' AND observation_kind = 'video_live_check'
  AND current_schema_version = 1 AND current_generation = 1;
COMMIT;
