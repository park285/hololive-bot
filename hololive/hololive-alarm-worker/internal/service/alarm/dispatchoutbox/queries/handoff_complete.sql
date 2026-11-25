/* fragment:handoff_fence */
, unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET handoff_next_check_at = NULL,
        handoff_checked_at = clock.now
    FROM clock CROSS JOIN held
    WHERE u.id = $1 AND held.value
)
SELECT checked.id, checked.status, checked.status_matches, TRUE AS owner_matches, checked.version_matches,
       TRUE AS lease_valid, checked.membership_matches, checked.members_non_terminal, checked.request_matches,
       passed.value AS applied, held.value AS check_held
FROM held
CROSS JOIN passed
LEFT JOIN checked ON TRUE
ORDER BY checked.id
