/* fragment:handoff_fence */
, updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'dlq',
        attempt_count = d.attempt_count + 1,
        dlq_at = clock.now,
        last_error = $6,
        last_error_code = $7,
        updated_at = clock.now
    FROM checked CROSS JOIN clock CROSS JOIN passed
    WHERE d.id = checked.id AND passed.value
    RETURNING d.id
), unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET handoff_next_check_at = NULL,
        handoff_checked_at = clock.now
    FROM clock CROSS JOIN held
    WHERE u.id = $1 AND held.value
)
SELECT checked.id, checked.status, checked.status_matches, TRUE AS owner_matches, checked.version_matches,
       TRUE AS lease_valid, checked.membership_matches, checked.members_non_terminal, checked.request_matches,
       updated.id IS NOT NULL AS applied, held.value AS check_held
FROM held
LEFT JOIN checked ON TRUE
LEFT JOIN updated ON updated.id = checked.id
ORDER BY checked.id
