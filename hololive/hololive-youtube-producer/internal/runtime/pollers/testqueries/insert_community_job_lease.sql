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
    lease_expires_at
) VALUES ($1, 'youtubejs', 'SUBJECT', 'community_collect', $2, $3, 60000,
          'ACTIVE', $4, $4, 1, $5, NOW() + INTERVAL '1 hour')
