CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_collisions_existing_observation
    ON source_observation_collisions (existing_observation_id);

