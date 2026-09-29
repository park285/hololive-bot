BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE OR REPLACE FUNCTION public.delete_retired_youtube_projection_batch(
    requested_cutoff TIMESTAMPTZ,
    requested_limit INTEGER
)
RETURNS TABLE (deleted_reasons BIGINT, deleted_targets BIGINT, deleted_generations BIGINT)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    chosen_generation BIGINT;
    locked_target_ctids TID[];
BEGIN
    deleted_reasons := 0;
    deleted_targets := 0;
    deleted_generations := 0;
    IF requested_cutoff IS NULL OR requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'invalid projection retention request' USING ERRCODE = '22023';
    END IF;

    -- generation의 UPDATE 잠금은 새 lease의 FK KEY SHARE와 충돌합니다.
    -- 잠금을 얻는 동안 commit된 lease가 있으면 새 문장의 snapshot으로 다시 확인하여
    -- 아직 참조되는 generation의 하위 데이터를 지우지 않습니다.
    SELECT generation.generation INTO chosen_generation
    FROM public.youtube_collection_projection_generations AS generation
    WHERE generation.status = 'RETIRED'
      AND generation.valid_until < requested_cutoff
      AND NOT EXISTS (
          SELECT 1 FROM public.youtube_collection_job_leases AS lease
          WHERE lease.projection_generation = generation.generation
      )
    ORDER BY generation.valid_until, generation.generation
    LIMIT 1
    FOR UPDATE OF generation SKIP LOCKED;

    IF chosen_generation IS NULL OR EXISTS (
        SELECT 1 FROM public.youtube_collection_job_leases AS lease
        WHERE lease.projection_generation = chosen_generation
    ) THEN
        RETURN NEXT;
        RETURN;
    END IF;

    WITH candidates AS (
        SELECT reason.projection_generation, reason.subject_key,
               reason.observation_kind, reason.reason_kind, reason.reason_key
        FROM public.youtube_collection_target_reasons AS reason
        WHERE reason.projection_generation = chosen_generation
        ORDER BY reason.subject_key, reason.observation_kind,
                 reason.reason_kind, reason.reason_key
        LIMIT requested_limit
        FOR UPDATE OF reason SKIP LOCKED
    )
    DELETE FROM public.youtube_collection_target_reasons AS reason
    USING candidates AS candidate
    WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind,
           reason.reason_kind, reason.reason_key) =
          (candidate.projection_generation, candidate.subject_key, candidate.observation_kind,
           candidate.reason_kind, candidate.reason_key);
    GET DIAGNOSTICS deleted_reasons = ROW_COUNT;

    -- target을 먼저 제한된 수만큼 잠급니다. reason INSERT의 FK KEY SHARE와 충돌하므로,
    -- 잠금을 기다리는 동안 추가된 reason까지 새 문장의 snapshot으로 확인할 수 있습니다.
    -- 자식이 없는 target만 지워 FK cascade가 배치 상한을 우회하지 않게 합니다.
    IF deleted_reasons < requested_limit THEN
        SELECT pg_catalog.array_agg(candidate.ctid) INTO locked_target_ctids
        FROM (
            SELECT target.ctid
            FROM public.youtube_collection_targets AS target
            WHERE target.projection_generation = chosen_generation
              AND NOT EXISTS (
                  SELECT 1 FROM public.youtube_collection_target_reasons AS reason
                  WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind) =
                        (target.projection_generation, target.subject_key, target.observation_kind)
              )
            ORDER BY target.subject_key, target.observation_kind
            LIMIT requested_limit - deleted_reasons
            FOR UPDATE OF target SKIP LOCKED
        ) AS candidate;

        DELETE FROM public.youtube_collection_targets AS target
        WHERE target.projection_generation = chosen_generation
          AND target.ctid = ANY(locked_target_ctids)
          AND NOT EXISTS (
              SELECT 1 FROM public.youtube_collection_target_reasons AS reason
              WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind) =
                    (target.projection_generation, target.subject_key, target.observation_kind)
          );
        GET DIAGNOSTICS deleted_targets = ROW_COUNT;
    END IF;

    DELETE FROM public.youtube_collection_projection_generations AS generation
    WHERE generation.generation = chosen_generation
      AND generation.status = 'RETIRED'
      AND generation.valid_until < requested_cutoff
      AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_job_leases AS lease
                      WHERE lease.projection_generation = generation.generation)
      AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_targets AS target
                      WHERE target.projection_generation = generation.generation);
    GET DIAGNOSTICS deleted_generations = ROW_COUNT;
    RETURN NEXT;
END
$$;

REVOKE ALL ON FUNCTION public.delete_retired_youtube_projection_batch(TIMESTAMPTZ, INTEGER) FROM PUBLIC;
DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.delete_retired_youtube_projection_batch(TIMESTAMPTZ, INTEGER)
            TO hololive_runtime;
    END IF;
END
$migration$;

-- 대형 테이블에만 vacuum을 조정하며 인스턴스 전체 주기나 다른 테이블은 바꾸지 않습니다.
ALTER TABLE public.source_observations SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_vacuum_threshold = 500,
    toast.autovacuum_vacuum_scale_factor = 0.05,
    toast.autovacuum_vacuum_threshold = 500
);
ALTER TABLE public.source_observation_applications SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_vacuum_threshold = 500
);

COMMIT;
