/* fragment:handoff_fence */
, updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'retry',
        attempt_count = d.attempt_count + 1,
        next_attempt_at = clock.now + ($6::BIGINT * INTERVAL '1 millisecond'),
        sent_at = NULL,
        last_error = $7,
        last_error_code = $8,
        updated_at = clock.now
    FROM checked CROSS JOIN clock CROSS JOIN passed
    WHERE d.id = checked.id AND passed.value
    RETURNING d.id
), unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET client_request_id = CASE WHEN passed.value THEN $9 ELSE u.client_request_id END,
        request_generation = CASE WHEN passed.value THEN u.request_generation + 1 ELSE u.request_generation END,
        handoff_request_id = CASE WHEN passed.value THEN NULL ELSE u.handoff_request_id END,
        handoff_next_check_at = NULL,
        handoff_checked_at = CASE WHEN passed.value THEN NULL ELSE clock.now END
    FROM clock CROSS JOIN passed CROSS JOIN held
    WHERE u.id = $1 AND held.value
)
SELECT checked.id, checked.status, checked.status_matches, TRUE AS owner_matches, checked.version_matches,
       TRUE AS lease_valid, checked.membership_matches, checked.members_non_terminal, checked.request_matches,
       updated.id IS NOT NULL AS applied, held.value AS check_held
FROM held
LEFT JOIN checked ON TRUE
LEFT JOIN updated ON updated.id = checked.id
ORDER BY checked.id
