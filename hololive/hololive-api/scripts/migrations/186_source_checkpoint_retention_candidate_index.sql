CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_collection_checkpoints_updated_identity
    ON public.source_collection_checkpoints (
        updated_at,
        provider,
        observation_kind,
        subject_key,
        scope_sha256 ASC
    );
