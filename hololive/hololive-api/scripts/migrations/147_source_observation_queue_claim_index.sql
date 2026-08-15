CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_queue_claim
    ON source_observation_queue (available_at, observation_id)
    WHERE status = 'PENDING';
