-- 직전 slot을 과거로 옮기고 next_due_at은 이전 긴 주기 기준의 미래 시각으로 둔다.
UPDATE youtube_collection_job_leases
SET scheduled_for = clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond'),
    next_due_at = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond')
WHERE job_key = $1
  AND slot_state = 'IDLE'
RETURNING scheduled_for
