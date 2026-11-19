WITH unit AS MATERIALIZED (
    SELECT u.id, u.client_request_id, u.request_body_hash, u.request_delivery_ids, u.request_generation
    FROM alarm_dispatch_send_units u
    WHERE u.id = $1
    FOR UPDATE
), input AS MATERIALIZED (
    SELECT input.id, input.attempt_count, input.retry_after_ms, input.error, input.error_code
    FROM jsonb_to_recordset($2::jsonb) AS input(
        id BIGINT, attempt_count INT, retry_after_ms BIGINT, error TEXT, error_code TEXT)
), locked AS MATERIALIZED (
    SELECT d.id, d.status, d.locked_by, d.attempt_count
    FROM alarm_dispatch_deliveries d
    WHERE d.send_unit_id = (SELECT unit.id FROM unit)
    ORDER BY d.id
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), unit_checked AS MATERIALIZED (
    SELECT members.ids = claimed.ids
               AND unit.request_delivery_ids @> claimed.ids AND unit.request_delivery_ids <@ claimed.ids
               AS membership_matches,
           NOT EXISTS (
               SELECT 1 FROM locked
               WHERE locked.status IN ('sent', 'dlq', 'quarantined', 'cancelled')
           ) AND unit.request_delivery_ids <@ members.ids AS members_non_terminal,
           unit.request_body_hash IS NOT NULL
               AND unit.client_request_id = $4
               AND unit.request_generation = $5 AS request_matches
    FROM unit
    CROSS JOIN (SELECT COALESCE(array_agg(locked.id ORDER BY locked.id), '{}') AS ids FROM locked) AS members
    CROSS JOIN (SELECT COALESCE(array_agg(input.id ORDER BY input.id), '{}') AS ids FROM input) AS claimed
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status,
           input.attempt_count AS next_attempt_count, input.retry_after_ms, input.error, input.error_code,
           locked.status = 'sending' AS status_matches,
           COALESCE(locked.locked_by = $3, FALSE) AS owner_matches,
           COALESCE(locked.attempt_count + 1 = input.attempt_count, FALSE) AS version_matches,
           COALESCE(unit_checked.membership_matches, FALSE) AS membership_matches,
           COALESCE(unit_checked.members_non_terminal, FALSE) AS members_non_terminal,
           COALESCE(unit_checked.request_matches, FALSE) AS request_matches
    FROM locked
    LEFT JOIN input ON input.id = locked.id
    LEFT JOIN unit_checked ON TRUE
), passed AS MATERIALIZED (
    SELECT EXISTS (SELECT 1 FROM checked) AND NOT EXISTS (
        SELECT 1 FROM checked
        WHERE NOT (checked.status_matches AND checked.owner_matches AND checked.version_matches
                   AND checked.membership_matches AND checked.members_non_terminal AND checked.request_matches)
    ) AS value
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'retry',
        attempt_count = checked.next_attempt_count,
        next_attempt_at = clock.now + (checked.retry_after_ms * INTERVAL '1 millisecond'),
        locked_by = NULL,
        locked_at = NULL,
        lock_expires_at = NULL,
        last_error = checked.error,
        last_error_code = checked.error_code,
        updated_at = clock.now
    FROM checked CROSS JOIN clock CROSS JOIN passed
    WHERE d.id = checked.id AND passed.value
    RETURNING d.id
), unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET client_request_id = $6,
        request_generation = $5 + 1,
        handoff_request_id = NULL,
        handoff_next_check_at = NULL,
        handoff_checked_at = NULL
    FROM passed
    WHERE u.id = $1 AND passed.value
)
SELECT checked.id, checked.status, checked.status_matches, checked.owner_matches, checked.version_matches,
       TRUE AS lease_valid, checked.membership_matches, checked.members_non_terminal, checked.request_matches,
       updated.id IS NOT NULL AS applied
FROM checked
LEFT JOIN updated ON updated.id = checked.id
ORDER BY checked.id
