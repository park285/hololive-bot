-- 318·319·320 뒤 점검 창에서 text 열을 제거한다. 제거 열의 공간은 뒤따르는 VACUUM (FULL)이 회수한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
ALTER TABLE public.source_observation_applications
    DROP COLUMN IF EXISTS entity_kind,
    DROP COLUMN IF EXISTS decision,
    DROP CONSTRAINT IF EXISTS chk_source_observation_application_bounds,
    ADD CONSTRAINT chk_source_observation_application_bounds
        CHECK (length(entity_key) BETWEEN 1 AND 256) NOT VALID;
COMMIT;
ALTER TABLE public.source_observation_applications VALIDATE CONSTRAINT chk_source_observation_application_bounds;

-- live session 보호는 사전 표의 entity kind 코드로 판정한다. 나머지 보호 사유는 317과 같다.
CREATE OR REPLACE FUNCTION public.source_observation_retention_protected(requested_id bigint)
RETURNS boolean LANGUAGE sql STABLE SET search_path = pg_catalog
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM public.source_observations AS observation
        WHERE observation.id = requested_id AND (
            EXISTS (SELECT 1 FROM public.source_observation_queue q WHERE q.observation_id = observation.id)
            OR EXISTS (SELECT 1 FROM public.source_observation_replay_requests r
                       WHERE r.observation_id = observation.id AND r.status = 'PENDING')
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
