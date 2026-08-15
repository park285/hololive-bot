BEGIN;

CREATE OR REPLACE FUNCTION public.delete_retired_youtube_collection_job_leases(
    requested_cutoff TIMESTAMPTZ,
    requested_limit INTEGER
)
RETURNS TABLE (deleted_job_key TEXT)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH candidate AS (
        SELECT lease.job_key
        FROM public.youtube_collection_job_leases AS lease
        JOIN public.youtube_collection_projection_generations AS generation
          ON generation.generation = lease.projection_generation
        WHERE generation.status = 'RETIRED'
          AND generation.valid_until < requested_cutoff
          AND (
              lease.slot_state <> 'ACTIVE'
              OR lease.lease_expires_at < clock_timestamp()
          )
        ORDER BY generation.generation, lease.job_key
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF lease SKIP LOCKED
    )
    DELETE FROM public.youtube_collection_job_leases AS lease
    USING candidate
    WHERE lease.job_key = candidate.job_key
    RETURNING lease.job_key
$$;

CREATE OR REPLACE FUNCTION public.delete_source_observation_retention_batch(
    requested_kinds TEXT[],
    requested_cutoffs TIMESTAMPTZ[],
    requested_limit INTEGER
)
RETURNS TABLE (deleted_id BIGINT)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH candidates AS (
        SELECT candidate.id
        FROM public.source_observations AS candidate
        JOIN pg_catalog.generate_subscripts(requested_kinds, 1) AS policy(position)
          ON requested_kinds[policy.position] = candidate.observation_kind
        LEFT JOIN public.source_observation_queue AS queue
          ON queue.observation_id = candidate.id
        WHERE pg_catalog.cardinality(requested_kinds) BETWEEN 1 AND 16
          AND pg_catalog.cardinality(requested_kinds) = pg_catalog.cardinality(requested_cutoffs)
          AND candidate.received_at < requested_cutoffs[policy.position]
          AND queue.observation_id IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM public.source_observation_replay_requests AS replay
              WHERE replay.observation_id = candidate.id
                AND replay.status = 'PENDING'
          )
          AND NOT EXISTS (
              SELECT 1
              FROM public.youtube_live_reconciliation_heads AS head
              WHERE head.end_candidate_observation_id = candidate.id
          )
        ORDER BY candidate.received_at, candidate.id
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF candidate SKIP LOCKED
    )
    DELETE FROM public.source_observations AS observation
    USING candidates
    WHERE observation.id = candidates.id
      AND NOT EXISTS (
          SELECT 1
          FROM public.source_observation_queue AS live_queue
          WHERE live_queue.observation_id = observation.id
      )
      AND NOT EXISTS (
          SELECT 1
          FROM public.source_observation_replay_requests AS live_replay
          WHERE live_replay.observation_id = observation.id
            AND live_replay.status = 'PENDING'
      )
      AND NOT EXISTS (
          SELECT 1
          FROM public.youtube_live_reconciliation_heads AS live_head
          WHERE live_head.end_candidate_observation_id = observation.id
      )
    RETURNING observation.id
$$;

REVOKE ALL ON FUNCTION public.delete_retired_youtube_collection_job_leases(TIMESTAMPTZ, INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.delete_source_observation_retention_batch(TEXT[], TIMESTAMPTZ[], INTEGER) FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.delete_retired_youtube_collection_job_leases(TIMESTAMPTZ, INTEGER) TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.delete_source_observation_retention_batch(TEXT[], TIMESTAMPTZ[], INTEGER) TO hololive_runtime;
    END IF;
END
$migration$;

COMMIT;
