CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observations_payload_id
    ON public.source_observations (payload_id);
