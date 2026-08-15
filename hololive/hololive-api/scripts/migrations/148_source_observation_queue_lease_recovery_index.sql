CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_queue_lease_recovery
    ON source_observation_queue (lease_expires_at, observation_id)
    WHERE status = 'PROCESSING';
