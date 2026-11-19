WITH unit AS MATERIALIZED (
    SELECT u.id
    FROM alarm_dispatch_send_units u
    WHERE u.id = $3
    FOR UPDATE
), input AS MATERIALIZED (
    SELECT input.id, input.attempt_count
    FROM jsonb_to_recordset($1::jsonb) AS input(id BIGINT, attempt_count INT)
), locked AS MATERIALIZED (
    SELECT d.id, d.status, d.locked_by, d.attempt_count, input.attempt_count AS claimed_attempt_count,
           COALESCE(d.send_unit_id = (SELECT unit.id FROM unit), FALSE) AS unit_matches
    FROM alarm_dispatch_deliveries d
    JOIN input ON input.id = d.id
    ORDER BY d.id
    FOR UPDATE OF d
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status,
           locked.status = 'sending' AS status_matches,
           COALESCE(locked.locked_by = $2, FALSE) AS owner_matches,
           locked.attempt_count = locked.claimed_attempt_count AS version_matches,
           locked.unit_matches AS membership_matches
    FROM locked
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'sent',
        sent_at = clock.now,
        locked_by = NULL,
        locked_at = NULL,
        lock_expires_at = NULL,
        updated_at = clock.now
    FROM checked CROSS JOIN clock
    WHERE d.id = checked.id
      AND checked.status_matches AND checked.owner_matches AND checked.version_matches AND checked.membership_matches
    RETURNING d.id
), unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET handoff_request_id = $4,
        handoff_next_check_at = clock.now + ($5::BIGINT * INTERVAL '1 millisecond'),
        handoff_checked_at = NULL
    FROM clock
    WHERE u.id = (SELECT unit.id FROM unit)
      AND (SELECT count(updated.id) FROM updated) = (SELECT count(input.id) FROM input)
)
SELECT checked.id, checked.status, checked.status_matches, checked.owner_matches, checked.version_matches,
       TRUE AS lease_valid, checked.membership_matches, TRUE AS members_non_terminal, TRUE AS request_matches,
       updated.id IS NOT NULL AS applied
FROM checked
LEFT JOIN updated ON updated.id = checked.id
ORDER BY checked.id
