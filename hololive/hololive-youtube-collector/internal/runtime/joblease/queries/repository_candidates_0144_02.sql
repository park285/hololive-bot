WITH target_bundles AS (
    SELECT subject_key,
           MIN(poll_interval_ms) AS min_interval_ms,
           MAX(poll_interval_ms) AS max_interval_ms,
           MAX(priority) AS max_priority
    FROM youtube_collection_targets
    WHERE projection_generation = $1
      AND observation_kind = ANY($2::text[])
      AND enabled = TRUE
      AND valid_until > statement_timestamp()
    GROUP BY subject_key
), due AS (
    SELECT target.subject_key,
           target.min_interval_ms,
           target.max_interval_ms,
           target.max_priority,
           lease.job_key,
           CASE
             WHEN lease.job_key IS NULL THEN '-infinity'::timestamptz
             WHEN lease.slot_state = 'IDLE' THEN lease.next_due_at
             WHEN lease.slot_state = 'DEFERRED' THEN lease.retry_not_before
             ELSE lease.lease_expires_at
           END AS effective_due_at
    FROM target_bundles AS target
    LEFT JOIN youtube_collection_job_leases AS lease
      ON lease.job_key = 'collector:' || $3 || ':' || $4 || ':' || target.subject_key
    WHERE ('collector:' || $3 || ':' || $4 || ':' || target.subject_key)
          <> ALL($5::text[])
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
LEFT JOIN (
    SELECT subject_key,
           min_interval_ms,
           max_interval_ms,
           job_key,
           max_priority,
           effective_due_at
    FROM due
    ORDER BY (job_key IS NOT NULL), max_priority DESC, effective_due_at ASC, subject_key ASC
    LIMIT $6 + 1
) AS due ON projection.is_current;
