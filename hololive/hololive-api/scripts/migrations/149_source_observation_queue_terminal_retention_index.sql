CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_queue_terminal_retention
    ON source_observation_queue (status, updated_at, observation_id)
    WHERE status IN ('PROCESSED', 'DEAD_LETTER');
