INSERT INTO youtube_collection_job_leases (
    job_key,
    provider,
    job_class,
    collection_job_kind,
    subject_key,
    projection_generation,
    poll_interval_ms,
    slot_state,
    scheduled_for,
    next_due_at,
    fence_epoch,
    owner_instance,
    lease_expires_at,
    membership_kinds,
    membership_exact_subject,
    membership_target_count
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, 'ACTIVE', $8, $8, $9, $10,
    clock_timestamp() + INTERVAL '1 hour', $11::text[], $12, $13
)
