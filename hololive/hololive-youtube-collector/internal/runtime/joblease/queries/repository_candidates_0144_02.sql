WITH target_bundles AS (
    SELECT subject_key,
           MIN(poll_interval_ms) AS min_interval_ms,
           MAX(poll_interval_ms) AS max_interval_ms,
           MAX(priority) AS max_priority
    FROM youtube_collection_targets
    WHERE projection_generation = $1
      AND observation_kind = ANY($2::text[])
      AND enabled = TRUE
      AND valid_until > clock_timestamp()
    GROUP BY subject_key
)
SELECT target.subject_key,
       target.min_interval_ms,
       target.max_interval_ms
FROM target_bundles AS target
LEFT JOIN youtube_collection_job_leases AS lease
  ON lease.job_key = 'collector:' || $4 || ':' || $5 || ':' || target.subject_key
WHERE lease.job_key IS NULL
   OR (lease.slot_state = 'IDLE' AND lease.next_due_at <= clock_timestamp())
   OR (lease.slot_state = 'DEFERRED' AND lease.retry_not_before <= clock_timestamp())
   OR (lease.slot_state = 'ACTIVE' AND lease.lease_expires_at <= clock_timestamp())
ORDER BY (lease.job_key IS NOT NULL), target.max_priority DESC, target.subject_key
LIMIT $3
