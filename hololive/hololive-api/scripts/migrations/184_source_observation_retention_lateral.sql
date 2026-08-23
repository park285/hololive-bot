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
    WITH policies AS (
        SELECT requested_kinds[policy.position] AS observation_kind,
               requested_cutoffs[policy.position] AS cutoff
        FROM pg_catalog.generate_subscripts(requested_kinds, 1) AS policy(position)
        WHERE pg_catalog.cardinality(requested_kinds) BETWEEN 1 AND 16
          AND pg_catalog.cardinality(requested_kinds) = pg_catalog.cardinality(requested_cutoffs)
    ),
    per_policy_candidates AS (
        SELECT candidate.id,
               candidate.received_at
        FROM policies AS policy
        CROSS JOIN LATERAL (
            SELECT observation.id,
                   observation.received_at
            FROM public.source_observations AS observation
            WHERE observation.observation_kind = policy.observation_kind
              AND observation.received_at < policy.cutoff
              AND NOT EXISTS (
                  SELECT 1
                  FROM public.source_observation_queue AS queue
                  WHERE queue.observation_id = observation.id
              )
              AND NOT EXISTS (
                  SELECT 1
                  FROM public.source_observation_replay_requests AS replay
                  WHERE replay.observation_id = observation.id
                    AND replay.status = 'PENDING'
              )
              AND NOT EXISTS (
                  SELECT 1
                  FROM public.youtube_live_reconciliation_heads AS head
                  WHERE head.end_candidate_observation_id = observation.id
              )
            ORDER BY observation.received_at, observation.id
            LIMIT CASE
                WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
                ELSE 0
            END
            FOR UPDATE OF observation SKIP LOCKED
        ) AS candidate
    ),
    candidates AS (
        SELECT candidate.id
        FROM per_policy_candidates AS candidate
        ORDER BY candidate.received_at, candidate.id
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
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
