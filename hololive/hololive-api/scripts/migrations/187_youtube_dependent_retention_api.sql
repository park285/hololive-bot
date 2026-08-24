BEGIN;

DO $migration$
DECLARE
    current_delete_action "char";
BEGIN
    SELECT constraint_row.confdeltype
    INTO current_delete_action
    FROM pg_catalog.pg_constraint AS constraint_row
    WHERE constraint_row.conrelid = 'public.youtube_live_viewer_sample_evidence'::regclass
      AND constraint_row.conname = 'youtube_live_viewer_sample_evidence_observation_id_fkey';

    IF current_delete_action IS NULL THEN
        RAISE EXCEPTION 'viewer sample evidence observation foreign key is missing';
    END IF;

    IF current_delete_action <> 'c' THEN
        ALTER TABLE public.youtube_live_viewer_sample_evidence
            DROP CONSTRAINT youtube_live_viewer_sample_evidence_observation_id_fkey;
        ALTER TABLE public.youtube_live_viewer_sample_evidence
            ADD CONSTRAINT youtube_live_viewer_sample_evidence_observation_id_fkey
            FOREIGN KEY (observation_id)
            REFERENCES public.source_observations(id)
            ON DELETE CASCADE
            NOT VALID;
    END IF;
END
$migration$;

CREATE OR REPLACE FUNCTION public.delete_source_observation_application_retention_batch(
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
               candidate.applied_at
        FROM policies AS policy
        CROSS JOIN LATERAL (
            SELECT application.id,
                   application.applied_at
            FROM public.source_observation_applications AS application
            WHERE application.observation_kind = policy.observation_kind
              AND application.observation_id IS NULL
              AND application.applied_at < policy.cutoff
            ORDER BY application.applied_at, application.id
            LIMIT CASE
                WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
                ELSE 0
            END
            FOR UPDATE OF application SKIP LOCKED
        ) AS candidate
    ),
    candidates AS (
        SELECT candidate.id
        FROM per_policy_candidates AS candidate
        ORDER BY candidate.applied_at, candidate.id
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
    )
    DELETE FROM public.source_observation_applications AS application
    USING candidates AS candidate
    WHERE application.id = candidate.id
      AND application.observation_id IS NULL
    RETURNING application.id
$$;

CREATE OR REPLACE FUNCTION public.delete_source_collection_checkpoint_retention_batch(
    requested_cutoff TIMESTAMPTZ,
    requested_limit INTEGER
)
RETURNS TABLE (deleted_scope_sha256 TEXT)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH candidates AS (
        SELECT checkpoint.provider,
               checkpoint.observation_kind,
               checkpoint.subject_key,
               checkpoint.scope_sha256
        FROM public.source_collection_checkpoints AS checkpoint
        WHERE checkpoint.updated_at < requested_cutoff
          AND EXISTS (
              SELECT 1
              FROM public.source_collection_checkpoints AS newer
              WHERE newer.provider = checkpoint.provider
                AND newer.observation_kind = checkpoint.observation_kind
                AND newer.subject_key = checkpoint.subject_key
                AND (newer.updated_at, newer.scope_sha256) >
                    (checkpoint.updated_at, checkpoint.scope_sha256)
          )
        ORDER BY checkpoint.updated_at,
                 checkpoint.provider,
                 checkpoint.observation_kind,
                 checkpoint.subject_key,
                 checkpoint.scope_sha256
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF checkpoint SKIP LOCKED
    )
    DELETE FROM public.source_collection_checkpoints AS checkpoint
    USING candidates AS candidate
    WHERE checkpoint.provider = candidate.provider
      AND checkpoint.observation_kind = candidate.observation_kind
      AND checkpoint.subject_key = candidate.subject_key
      AND checkpoint.scope_sha256 = candidate.scope_sha256
      AND checkpoint.updated_at < requested_cutoff
      AND EXISTS (
          SELECT 1
          FROM public.source_collection_checkpoints AS newer
          WHERE newer.provider = checkpoint.provider
            AND newer.observation_kind = checkpoint.observation_kind
            AND newer.subject_key = checkpoint.subject_key
            AND (newer.updated_at, newer.scope_sha256) >
                (checkpoint.updated_at, checkpoint.scope_sha256)
      )
    RETURNING checkpoint.scope_sha256
$$;

REVOKE ALL ON FUNCTION public.delete_source_observation_application_retention_batch(
    TEXT[], TIMESTAMPTZ[], INTEGER
) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.delete_source_collection_checkpoint_retention_batch(
    TIMESTAMPTZ, INTEGER
) FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.delete_source_observation_application_retention_batch(
            TEXT[], TIMESTAMPTZ[], INTEGER
        ) TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.delete_source_collection_checkpoint_retention_batch(
            TIMESTAMPTZ, INTEGER
        ) TO hololive_runtime;
    END IF;
END
$migration$;

COMMIT;

ALTER TABLE public.youtube_live_viewer_sample_evidence
    VALIDATE CONSTRAINT youtube_live_viewer_sample_evidence_observation_id_fkey;
