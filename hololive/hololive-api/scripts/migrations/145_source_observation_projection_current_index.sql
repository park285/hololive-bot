CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_youtube_collection_projection_one_current
    ON youtube_collection_projection_generations ((status))
    WHERE status = 'CURRENT';
