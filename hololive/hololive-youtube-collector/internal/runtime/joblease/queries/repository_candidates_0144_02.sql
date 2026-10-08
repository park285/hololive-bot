WITH target_bundles AS (
    SELECT subject_key,
           MIN(poll_interval_ms) AS min_interval_ms,
           MAX(poll_interval_ms) AS max_interval_ms,
           MAX(priority) AS max_priority
    FROM youtube_collection_targets
    WHERE projection_generation = $1
      AND observation_kind = ANY($2::text[])
      AND enabled = TRUE
    GROUP BY subject_key
    -- not_before는 신규 입장 판정에만 쓴다. bundle의 한 행이라도 입장 가능하면 후보다.
    HAVING bool_or(not_before IS NULL OR not_before <= statement_timestamp())
), due AS (
    SELECT target.subject_key,
           target.min_interval_ms,
           target.max_interval_ms,
           target.max_priority,
           lease.job_key,
           CASE
             WHEN lease.job_key IS NULL THEN '-infinity'::timestamptz
             -- 주기가 줄었으면 이전 긴 주기로 계산된 next_due_at 대신 현재 주기의 다음 slot을 due로 본다.
             WHEN lease.slot_state = 'IDLE' THEN LEAST(
                 lease.next_due_at,
                 lease.scheduled_for + target.min_interval_ms * INTERVAL '1 millisecond'
             )
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
        OR (lease.slot_state = 'IDLE' AND LEAST(
                lease.next_due_at,
                lease.scheduled_for + target.min_interval_ms * INTERVAL '1 millisecond'
            ) <= statement_timestamp())
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
    -- 새 수명 검토 대상의 최초 discovery가 기존 LIVE 확인을 밀어내지 않는다.
    -- 다른 job 종류의 현행 신규 대상 우선순위는 유지한다.
    ORDER BY CASE WHEN $4 = 'youtubejs_video_live' THEN max_priority ELSE 0 END DESC,
             (job_key IS NOT NULL), max_priority DESC, effective_due_at ASC, subject_key ASC
    LIMIT $6 + 1
) AS due ON projection.is_current;
