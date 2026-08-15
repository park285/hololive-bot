BEGIN;

CREATE OR REPLACE FUNCTION public.lock_youtube_collection_projection(requested_generation BIGINT)
RETURNS TABLE (generation BIGINT)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT projection.generation
    FROM public.youtube_collection_projection_generations AS projection
    WHERE projection.generation = requested_generation
      AND projection.status = 'CURRENT'
      AND projection.valid_until > clock_timestamp()
    FOR SHARE OF projection
$$;

CREATE OR REPLACE FUNCTION public.lock_observation_contract(
    requested_provider TEXT,
    requested_observation_kind TEXT
)
RETURNS TABLE (
    current_schema_version SMALLINT,
    current_generation BIGINT
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT contract.current_schema_version,
           contract.current_generation
    FROM public.observation_contract_generations AS contract
    WHERE contract.provider = requested_provider
      AND contract.observation_kind = requested_observation_kind
    FOR SHARE OF contract
$$;

CREATE OR REPLACE FUNCTION public.lock_source_observation_identity(
    requested_provider TEXT,
    requested_observation_kind TEXT,
    requested_subject_key TEXT,
    requested_observation_key TEXT,
    requested_schema_version SMALLINT,
    requested_contract_generation BIGINT
)
RETURNS TABLE (
    id BIGINT,
    evidence_sha256 TEXT
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT observation.id,
           observation.evidence_sha256
    FROM public.source_observations AS observation
    WHERE observation.provider = requested_provider
      AND observation.observation_kind = requested_observation_kind
      AND observation.subject_key = requested_subject_key
      AND observation.observation_key = requested_observation_key
      AND observation.schema_version = requested_schema_version
      AND observation.contract_generation = requested_contract_generation
    FOR SHARE OF observation
$$;

CREATE OR REPLACE FUNCTION public.lock_source_observation(requested_observation_id BIGINT)
RETURNS TABLE (
    provider TEXT,
    observation_kind TEXT,
    subject_key TEXT,
    observation_key TEXT,
    schema_version SMALLINT,
    contract_generation BIGINT,
    evidence_sha256 TEXT
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT observation.provider,
           observation.observation_kind,
           observation.subject_key,
           observation.observation_key,
           observation.schema_version,
           observation.contract_generation,
           observation.evidence_sha256
    FROM public.source_observations AS observation
    WHERE observation.id = requested_observation_id
    FOR SHARE OF observation
$$;

REVOKE ALL ON FUNCTION public.lock_youtube_collection_projection(BIGINT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.lock_observation_contract(TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.lock_source_observation_identity(TEXT, TEXT, TEXT, TEXT, SMALLINT, BIGINT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.lock_source_observation(BIGINT) FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT EXECUTE ON FUNCTION public.lock_youtube_collection_projection(BIGINT) TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.lock_observation_contract(TEXT, TEXT) TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.lock_source_observation_identity(TEXT, TEXT, TEXT, TEXT, SMALLINT, BIGINT) TO hololive_scraper;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.lock_source_observation(BIGINT) TO hololive_runtime;
    END IF;
END
$migration$;

COMMIT;
