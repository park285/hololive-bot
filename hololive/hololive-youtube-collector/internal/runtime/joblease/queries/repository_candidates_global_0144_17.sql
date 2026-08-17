WITH target_bundle AS (
    SELECT COUNT(subject_key) AS target_count,
           MIN(poll_interval_ms) AS min_interval_ms,
           MAX(poll_interval_ms) AS max_interval_ms,
           MAX(priority) AS max_priority
    FROM youtube_collection_targets
    WHERE projection_generation = $1
      AND observation_kind = ANY($2::text[])
      AND enabled = TRUE
      AND valid_until > statement_timestamp()
      AND (NOT $3::boolean OR subject_key = $4)
), identity AS (
    SELECT $5::text AS job_key,
           $4::text AS subject_key,
           target_bundle.target_count,
           target_bundle.min_interval_ms,
           target_bundle.max_interval_ms,
           target_bundle.max_priority
    FROM target_bundle
), due AS (
    SELECT identity.job_key,
           identity.subject_key,
           identity.target_count,
           identity.min_interval_ms,
           identity.max_interval_ms,
           identity.max_priority,
           lease.slot_state,
           lease.next_due_at,
           lease.retry_not_before,
           lease.lease_expires_at
    FROM identity
    LEFT JOIN youtube_collection_job_leases AS lease
      ON lease.job_key = identity.job_key
    WHERE identity.target_count > 0
      AND identity.job_key <> ALL($6::text[])
      AND (
           lease.job_key IS NULL
        OR (lease.slot_state = 'IDLE' AND lease.next_due_at <= statement_timestamp())
        OR (lease.slot_state = 'DEFERRED' AND lease.retry_not_before <= statement_timestamp())
        OR (lease.slot_state = 'ACTIVE' AND lease.lease_expires_at <= statement_timestamp())
      )
), projection AS (
    SELECT EXISTS (
        SELECT 1
        FROM youtube_collection_projection_generations
        WHERE generation = $1
          AND status = 'CURRENT'
          AND valid_until > statement_timestamp()
    ) AS is_current
)
SELECT projection.is_current,
       due.subject_key,
       due.min_interval_ms,
       due.max_interval_ms
FROM projection
LEFT JOIN due ON projection.is_current;
