CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_collection_projection_retired_retention
    ON youtube_collection_projection_generations (valid_until, generation)
    WHERE status = 'RETIRED';
