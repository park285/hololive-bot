CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_collisions_occurred
    ON source_observation_collisions (occurred_at, id);
