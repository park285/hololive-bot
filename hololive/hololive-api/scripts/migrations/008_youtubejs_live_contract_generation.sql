-- baseline은 YouTube.js live_snapshot 1/2·video_live_check 1/1을 시드하지만 현행 collector와 API는 1/3·2/2만 주고받는다.
-- 구세대 행만 올리므로 이미 전환된 DB에서는 바뀌는 행이 없다. 구세대 관측이 queue에 남아 있으면 의미가 바뀌므로 중단한다.
BEGIN;
SET LOCAL lock_timeout = '3s';

DO $cutover$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.observation_contract_generations
        WHERE provider = 'youtubejs'
          AND ((observation_kind = 'live_snapshot' AND current_schema_version = 1 AND current_generation = 2)
            OR (observation_kind = 'video_live_check' AND current_schema_version = 1 AND current_generation = 1))
    ) AND EXISTS (
        SELECT 1 FROM public.source_observations observation
        JOIN public.source_observation_queue queue ON queue.observation_id = observation.id
        WHERE observation.provider = 'youtubejs'
          AND observation.observation_kind IN ('live_snapshot', 'video_live_check')
          AND queue.status IN ('PENDING', 'PROCESSING')
    ) THEN
        RAISE EXCEPTION 'youtubejs live contract generation upgrade requires drained snapshot/video check queue';
    END IF;
END
$cutover$;

UPDATE public.observation_contract_generations
SET current_generation = 3, updated_by = 'migration-008', updated_at = now()
WHERE provider = 'youtubejs' AND observation_kind = 'live_snapshot'
  AND current_schema_version = 1 AND current_generation = 2;

UPDATE public.observation_contract_generations
SET current_schema_version = 2, current_generation = 2, updated_by = 'migration-008', updated_at = now()
WHERE provider = 'youtubejs' AND observation_kind = 'video_live_check'
  AND current_schema_version = 1 AND current_generation = 1;

COMMIT;
