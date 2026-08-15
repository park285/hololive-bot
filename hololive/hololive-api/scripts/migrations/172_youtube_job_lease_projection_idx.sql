CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_collection_job_projection_generation
    ON youtube_collection_job_leases (projection_generation, job_key);
