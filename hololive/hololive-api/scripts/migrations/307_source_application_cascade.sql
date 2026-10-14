-- 감사 수명은 원본과 같다. publisher/consumer/retention을 배수한 점검 창에서 적용한다.
-- 고아는 PK keyset으로 최대 1000행씩 삭제하고 실제 COMMIT한다. 재시작은 남은 행부터 안전하다.
CREATE OR REPLACE PROCEDURE public.purge_source_application_orphans_v307()
LANGUAGE plpgsql
AS $procedure$
DECLARE
    last_id bigint := 0;
    picked_ids bigint[];
BEGIN
    LOOP
        PERFORM set_config('lock_timeout', '3s', true);
        SELECT array_agg(id ORDER BY id) INTO picked_ids FROM (
            SELECT id FROM public.source_observation_applications
            WHERE id > last_id ORDER BY id LIMIT 1000
        ) AS batch;
        EXIT WHEN picked_ids IS NULL;
        DELETE FROM public.source_observation_applications
        WHERE id = ANY(picked_ids) AND observation_id IS NULL;
        last_id := picked_ids[cardinality(picked_ids)];
        COMMIT;
    END LOOP;
END
$procedure$;
REVOKE ALL ON PROCEDURE public.purge_source_application_orphans_v307() FROM PUBLIC;
CALL public.purge_source_application_orphans_v307();
DROP PROCEDURE public.purge_source_application_orphans_v307();

DROP FUNCTION IF EXISTS public.delete_source_observation_application_retention_batch(text[], timestamptz[], integer);

BEGIN;
SET LOCAL lock_timeout = '3s';
ALTER TABLE public.source_observation_applications
    DROP CONSTRAINT IF EXISTS source_observation_applications_observation_id_fkey,
    ADD CONSTRAINT source_observation_applications_observation_id_fkey
        FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE CASCADE NOT VALID,
    DROP CONSTRAINT IF EXISTS chk_source_observation_application_bounds,
    DROP COLUMN IF EXISTS provider,
    DROP COLUMN IF EXISTS observation_kind,
    DROP COLUMN IF EXISTS subject_key,
    DROP COLUMN IF EXISTS evidence_sha256,
    DROP COLUMN IF EXISTS effective_at,
    ADD CONSTRAINT chk_source_observation_application_bounds CHECK (
        length(entity_kind) BETWEEN 1 AND 64 AND length(entity_key) BETWEEN 1 AND 256
        AND length(decision) BETWEEN 1 AND 128
    ) NOT VALID;
DO $migration$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'public.source_observation_applications'::regclass
                     AND conname = 'source_application_observation_nn') THEN
        ALTER TABLE public.source_observation_applications
            ADD CONSTRAINT source_application_observation_nn CHECK (observation_id IS NOT NULL) NOT VALID;
    END IF;
END
$migration$;
COMMIT;

ALTER TABLE public.source_observation_applications VALIDATE CONSTRAINT source_observation_applications_observation_id_fkey;
ALTER TABLE public.source_observation_applications VALIDATE CONSTRAINT chk_source_observation_application_bounds;
ALTER TABLE public.source_observation_applications VALIDATE CONSTRAINT source_application_observation_nn;
BEGIN;
SET LOCAL lock_timeout = '3s';
ALTER TABLE public.source_observation_applications ALTER COLUMN observation_id SET NOT NULL;
ALTER TABLE public.source_observation_applications DROP CONSTRAINT IF EXISTS source_application_observation_nn;
COMMIT;

-- 원본 보존의 모든 진입점은 같은 보호 판정을 사용한다. 큐 상태와 무관하게 큐가 남으면 보존한다.
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
                       WHERE l.job_key = observation.job_key AND l.slot_state = 'ACTIVE')
            OR EXISTS (SELECT 1 FROM public.source_observation_applications a
                       JOIN public.youtube_live_sessions s ON s.video_id = a.entity_key
                       WHERE a.observation_id = observation.id AND a.entity_kind = 'youtube_live_session'
                         AND s.status IN ('LIVE', 'UPCOMING'))
            OR EXISTS (SELECT 1 FROM public.youtube_live_pending_ends p
                       JOIN public.youtube_live_sessions s ON s.video_id = p.video_id
                       WHERE p.observation_id = observation.id AND s.status IN ('LIVE', 'UPCOMING'))
        )
    )
$function$;

CREATE OR REPLACE FUNCTION public.delete_source_observation_retention_batch(
    requested_kinds text[], requested_cutoffs timestamptz[], requested_limit integer
) RETURNS TABLE (deleted_id bigint)
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog SET jit = off
AS $function$
    WITH policies AS (
        SELECT requested_kinds[p.position] AS kind, requested_cutoffs[p.position] AS cutoff
        FROM pg_catalog.generate_subscripts(requested_kinds, 1) AS p(position)
        WHERE cardinality(requested_kinds) BETWEEN 1 AND 16
          AND cardinality(requested_kinds) = cardinality(requested_cutoffs)
    ), per_policy AS (
        SELECT candidate.id, candidate.received_at FROM policies
        CROSS JOIN LATERAL (
            SELECT observation.id, observation.received_at
            FROM public.source_observations observation
            WHERE observation.observation_kind = policies.kind AND observation.received_at < policies.cutoff
              AND NOT public.source_observation_retention_protected(observation.id)
            ORDER BY observation.received_at, observation.id
            LIMIT CASE WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit ELSE 0 END
            FOR UPDATE OF observation SKIP LOCKED
        ) candidate
    ), candidates AS (
        SELECT id FROM per_policy ORDER BY received_at, id
        LIMIT CASE WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit ELSE 0 END
    )
    DELETE FROM public.source_observations observation USING candidates
    WHERE observation.id = candidates.id
      AND NOT public.source_observation_retention_protected(observation.id)
    RETURNING observation.id
$function$;

REVOKE ALL ON FUNCTION public.source_observation_retention_protected(bigint) FROM PUBLIC;
DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.source_observation_retention_protected(bigint) TO hololive_runtime;
        REVOKE DELETE ON public.source_observation_applications FROM hololive_runtime;
    END IF;
END
$migration$;
