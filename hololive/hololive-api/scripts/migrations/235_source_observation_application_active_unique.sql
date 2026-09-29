-- Non-transactional: leave the existing constraint in place while building the replacement.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_source_observation_application_active
    ON public.source_observation_applications (observation_id, entity_kind, entity_key)
    WHERE observation_id IS NOT NULL;
