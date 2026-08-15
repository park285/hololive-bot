INSERT INTO youtube_collection_job_leases (
    job_key,
    provider,
    job_class,
    collection_job_kind,
    subject_key,
    projection_generation,
    poll_interval_ms,
    scheduled_for,
    next_due_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, clock_timestamp(), clock_timestamp())
ON CONFLICT (job_key) DO NOTHING
