-- 발송 전 claim을 같은 attempt로 반환한다. 소유자가 만료 전에 스스로 돌려주는 전이라 lease 만료는 보지 않는다.
WITH input AS MATERIALIZED (
    SELECT input.id, input.attempt_count
    FROM jsonb_to_recordset($1::jsonb) AS input(id BIGINT, attempt_count INT)
), locked AS MATERIALIZED (
    SELECT d.id, d.status, d.locked_by, d.attempt_count, input.attempt_count AS claimed_attempt_count
    FROM alarm_dispatch_deliveries d
    JOIN input ON input.id = d.id
    ORDER BY d.id
    FOR UPDATE OF d
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status,
           locked.status = 'leased' AS status_matches,
           COALESCE(locked.locked_by = $2, FALSE) AS owner_matches,
           locked.attempt_count = locked.claimed_attempt_count AS version_matches
    FROM locked
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'retry',
        next_attempt_at = clock.now,
        locked_by = NULL,
        locked_at = NULL,
        lock_expires_at = NULL,
        last_error = 'lease released before external send',
        last_error_code = 'lease_released',
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
