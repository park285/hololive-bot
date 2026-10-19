-- 외부 호출 뒤 실패를 retry·dlq로 정산한다. 확정된 외부 결과를 lease 만료로 되돌리지 않으므로 lease는 보지 않는다.
-- 만료 회수(RecoverExpiredLeased)는 leased만, stale 격리는 sending의 locked_by를 비우므로 소유자 일치가 소유권 증명이다.
WITH input AS MATERIALIZED (
    SELECT input.id, input.attempt_count, input.retry_after_ms, input.error, input.error_code,
           input.target_status, input.mark_dlq
    FROM jsonb_to_recordset($1::jsonb) AS input(
        id BIGINT, attempt_count INT, retry_after_ms BIGINT,
        error TEXT, error_code TEXT, target_status TEXT, mark_dlq BOOLEAN)
), locked AS MATERIALIZED (
    SELECT d.id, d.status, d.locked_by, d.attempt_count,
           input.attempt_count AS next_attempt_count, input.retry_after_ms, input.error, input.error_code,
           input.target_status, input.mark_dlq
    FROM alarm_dispatch_deliveries d
    JOIN input ON input.id = d.id
    ORDER BY d.id
    FOR UPDATE OF d
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status, locked.next_attempt_count, locked.retry_after_ms, locked.error,
           locked.error_code, locked.target_status, locked.mark_dlq,
           locked.status IN ('leased', 'sending') AS status_matches,
           COALESCE(locked.locked_by = $2, FALSE) AS owner_matches,
           locked.attempt_count + 1 = locked.next_attempt_count AS version_matches
    FROM locked
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = checked.target_status,
        attempt_count = checked.next_attempt_count,
        next_attempt_at = CASE
            WHEN checked.retry_after_ms IS NULL THEN d.next_attempt_at
            ELSE clock.now + (checked.retry_after_ms * INTERVAL '1 millisecond')
        END,
        dlq_at = CASE WHEN checked.mark_dlq THEN clock.now ELSE d.dlq_at END,
        locked_by = NULL,
        locked_at = NULL,
        lock_expires_at = NULL,
        last_error = checked.error,
        last_error_code = checked.error_code,
        updated_at = clock.now
    FROM checked CROSS JOIN clock
    WHERE d.id = checked.id
      AND checked.status_matches AND checked.owner_matches AND checked.version_matches
    RETURNING d.id
)
SELECT checked.id, checked.status, checked.status_matches, checked.owner_matches, checked.version_matches,
       TRUE AS lease_valid, TRUE AS membership_matches, TRUE AS members_non_terminal, TRUE AS request_matches,
       updated.id IS NOT NULL AS applied
FROM checked
LEFT JOIN updated ON updated.id = checked.id
ORDER BY checked.id
