CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_collection_job_due
    ON youtube_collection_job_leases (
        slot_state,
        next_due_at,
        retry_not_before,
        lease_expires_at,
        job_key
    );
