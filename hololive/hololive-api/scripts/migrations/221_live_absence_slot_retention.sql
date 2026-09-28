-- 오래된 absence slot은 더 이상 과거 positive 재처리에 쓰지 않는다.
-- canonical session/head와 pending end에는 이미 반영된 판정 사실이 남는다.
CREATE OR REPLACE FUNCTION public.delete_youtube_live_absence_slot_retention_batch(
    requested_cutoff TIMESTAMPTZ,
    requested_limit INTEGER
)
RETURNS TABLE (deleted_id BIGINT)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH active_old_live AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PENDING'
          AND observation.observation_kind = 'live_snapshot'
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    processing_old_live AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PROCESSING'
          AND observation.observation_kind = 'live_snapshot'
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    replaying_old_live AS (
        SELECT 1
        FROM public.source_observation_replay_requests AS replay
        JOIN public.source_observations AS observation
          ON observation.id = replay.observation_id
        WHERE replay.status = 'PENDING'
          AND observation.observation_kind = 'live_snapshot'
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    candidates AS (
        SELECT slot.observation_id
        FROM public.youtube_live_absence_slots AS slot
        WHERE slot.scheduled_for < requested_cutoff
          AND NOT EXISTS (SELECT 1 FROM active_old_live)
          AND NOT EXISTS (SELECT 1 FROM processing_old_live)
          AND NOT EXISTS (SELECT 1 FROM replaying_old_live)
        ORDER BY slot.scheduled_for, slot.observation_id
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF slot SKIP LOCKED
    )
    DELETE FROM public.youtube_live_absence_slots AS slot
    USING candidates AS candidate
    WHERE slot.observation_id = candidate.observation_id
      AND slot.scheduled_for < requested_cutoff
    RETURNING slot.observation_id
$$;

REVOKE ALL ON FUNCTION public.delete_youtube_live_absence_slot_retention_batch(
    TIMESTAMPTZ, INTEGER
) FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.delete_youtube_live_absence_slot_retention_batch(
            TIMESTAMPTZ, INTEGER
        ) TO hololive_runtime;
    END IF;
END
$migration$;
