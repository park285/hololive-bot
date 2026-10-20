-- 전이 없이 다음 확인을 clock + $5 밀리초로 예약하되 admission(멤버 sent_at) + 지평 $6 밀리초를 넘기지 않는다. 지평
-- 시각의 확인이 마지막 확인이다. 멤버 조건이 어긋났으면 확인을 닫는다. applied는 멤버 전원의 조건이 맞았는지다.
/* handoff_fence */, unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET handoff_next_check_at = CASE
            WHEN passed.value THEN LEAST(
                clock.now + ($5::BIGINT * INTERVAL '1 millisecond'),
                admitted.at + ($6::BIGINT * INTERVAL '1 millisecond'))
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
