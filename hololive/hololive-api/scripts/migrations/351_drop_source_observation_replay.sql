-- source observation 수동 replay 파이프라인을 지운다. 요청 행을 넣던 RequestReplay에는 운영 생산자(CLI·route)가
-- 없었고, 소비 loop(YOUTUBE_PLANE_REPLAY_*)도 기본값으로 꺼져 있었다. 이 migration과 같은 변경에서 RequestReplay,
-- ProcessNextReplay loop, replay SQL과 YOUTUBE_PLANE_REPLAY_* env를 지웠다. replay epoch(191)는 claim 시점의
-- pre-epoch dead-letter가 계속 쓰므로 남긴다.
-- 처리되지 않은 PENDING 요청이 있으면 운영자의 재처리 의도를 말없이 버리게 되므로 지우지 않고 실패한다.
-- 이전 이미지는 retention에서 이 테이블을 지우고 replay loop에서 읽으므로 이 migration 뒤로는 그 이미지로 되돌리지 않는다.

-- retention 보호와 live absence slot 정리에서 pending replay 조건만 뺀다. 나머지 보호 사유는 321·221과 같다.
CREATE OR REPLACE FUNCTION public.source_observation_retention_protected(requested_id bigint)
RETURNS boolean LANGUAGE sql STABLE SET search_path = pg_catalog
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM public.source_observations AS observation
        WHERE observation.id = requested_id AND (
            EXISTS (SELECT 1 FROM public.source_observation_queue q WHERE q.observation_id = observation.id)
            OR EXISTS (SELECT 1 FROM public.youtube_live_reconciliation_heads h
                       WHERE h.end_candidate_observation_id = observation.id)
            OR EXISTS (SELECT 1 FROM public.youtube_collection_projection_generations g
                       WHERE g.generation = observation.projection_generation AND g.status IN ('CURRENT', 'STAGING'))
            OR EXISTS (SELECT 1 FROM public.youtube_collection_job_leases l
                       JOIN public.source_observation_provenances p ON p.id = observation.provenance_id
                       WHERE l.job_key = p.job_key AND l.slot_state = 'ACTIVE')
            OR EXISTS (SELECT 1 FROM public.source_observation_applications a
                       JOIN public.source_observation_entity_kinds k ON k.id = a.entity_kind_id
                       JOIN public.youtube_live_sessions s ON s.video_id = a.entity_key
                       WHERE a.observation_id = observation.id AND k.name = 'youtube_live_session'
                         AND s.status IN ('LIVE', 'UPCOMING'))
            OR EXISTS (SELECT 1 FROM public.youtube_live_pending_ends p
                       JOIN public.youtube_live_sessions s ON s.video_id = p.video_id
                       WHERE p.observation_id = observation.id AND s.status IN ('LIVE', 'UPCOMING'))
        )
    )
$function$;

-- CREATE OR REPLACE는 221의 소유자·SECURITY DEFINER 권한과 hololive_runtime EXECUTE 권한을 그대로 둔다.
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
    candidates AS (
        SELECT slot.observation_id
        FROM public.youtube_live_absence_slots AS slot
        WHERE slot.scheduled_for < requested_cutoff
          AND NOT EXISTS (SELECT 1 FROM active_old_live)
          AND NOT EXISTS (SELECT 1 FROM processing_old_live)
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

-- 요청 표와 그 sequence·index·grant를 함께 지우고, replay만 올리던 queue.replay_count 열도 지운다.
-- 열 삭제는 카탈로그만 바꾸므로 잠금은 짧고, 공간은 이후 행 갱신과 VACUUM이 회수한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
BEGIN
    -- 재적용 때는 표가 이미 없으므로 존재를 먼저 확인한 뒤에만 표를 읽는다.
    IF to_regclass('public.source_observation_replay_requests') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM public.source_observation_replay_requests WHERE status = 'PENDING') THEN
            RAISE EXCEPTION 'source_observation_replay_requests has PENDING rows; resolve them before dropping replay';
        END IF;
    END IF;
END
$migration$;
DROP TABLE IF EXISTS public.source_observation_replay_requests;
ALTER TABLE public.source_observation_queue DROP COLUMN IF EXISTS replay_count;
COMMIT;
