WITH input AS MATERIALIZED (
    SELECT id, attempt_count, next_attempt_at, error, error_code, target_status, mark_dlq
    FROM jsonb_to_recordset($1::jsonb) AS input(
        id BIGINT, attempt_count INT, next_attempt_at TIMESTAMPTZ,
        error TEXT, error_code TEXT, target_status TEXT, mark_dlq BOOLEAN)
), locked AS MATERIALIZED (
    SELECT d.id, d.lock_expires_at, input.attempt_count, input.next_attempt_at,
           input.error, input.error_code, input.target_status, input.mark_dlq
    FROM alarm_dispatch_deliveries d JOIN input ON input.id = d.id
    WHERE input.attempt_count = d.attempt_count + 1
      AND d.status = 'leased'
      AND d.locked_by = $2
    ORDER BY d.id
    FOR UPDATE OF d
), clock AS MATERIALIZED (
    -- 배치 뒤쪽 행의 잠금 대기까지 끝내야 앞쪽 행에도 만료 전 시각을 재사용하지 않는다.
    SELECT clock_timestamp() AS transitioned_at
    FROM (SELECT count(id) AS locked_count FROM locked) AS acquired
    WHERE acquired.locked_count > 0
)
UPDATE alarm_dispatch_deliveries d
SET status = locked.target_status, attempt_count = locked.attempt_count,
    next_attempt_at = COALESCE(locked.next_attempt_at, d.next_attempt_at),
    dlq_at = CASE WHEN locked.mark_dlq THEN clock.transitioned_at ELSE d.dlq_at END,
    locked_by = NULL, locked_at = NULL, lock_expires_at = NULL,
    last_error = locked.error, last_error_code = locked.error_code, updated_at = clock.transitioned_at
FROM locked CROSS JOIN clock
WHERE d.id = locked.id
  AND locked.lock_expires_at > clock.transitioned_at
RETURNING d.id
