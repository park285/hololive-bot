-- handoff_completed를 확인으로 기록한다. 멤버는 sent 그대로이고 단위의 다음 확인을 지우고 확인 시각을 남긴다.
-- 확인 소유가 남았으면 멤버 조건과 무관하게 확인을 닫는다. applied는 멤버 전원의 조건이 맞았는지다.
/* handoff_fence */, unit_updated AS (
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
