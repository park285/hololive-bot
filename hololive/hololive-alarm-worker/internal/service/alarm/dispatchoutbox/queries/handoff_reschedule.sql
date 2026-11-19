/* fragment:handoff_fence */, unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET handoff_next_check_at = CASE
            WHEN passed.value THEN LEAST(
                clock.now + ($6::BIGINT * INTERVAL '1 millisecond'),
                admitted.at + ($7::BIGINT * INTERVAL '1 millisecond'))
        END,
        handoff_checked_at = CASE WHEN passed.value THEN NULL ELSE clock.now END
    FROM clock
    CROSS JOIN passed
    CROSS JOIN held
    CROSS JOIN (SELECT min(locked.sent_at) AS at FROM locked) AS admitted
    WHERE u.id = $1 AND held.value
)
SELECT checked.id, checked.status, checked.status_matches, TRUE AS owner_matches, checked.version_matches,
       TRUE AS lease_valid, checked.membership_matches, checked.members_non_terminal, checked.request_matches,
       passed.value AS applied, held.value AS check_held
FROM held
CROSS JOIN passed
LEFT JOIN checked ON TRUE
ORDER BY checked.id
