-- Iris가 dispatch 전에 포기한(failed) 요청을 새 세대와 멤버 전원의 retry 전이로 한 문장에 기록한다. 멤버는
-- attempt_count + 1, next_attempt_at = clock + $5 밀리초, sent_at 없음으로 돌아가고 단위는 새 ID($8)·세대 + 1과
-- 빈 handoff 확인 상태가 된다. 다음 claim은 고정 본문을 새 ID로 다시 보낸다. 멤버 조건이 어긋났으면 멤버를 바꾸지
-- 않고 확인을 닫는다.
/* handoff_fence */, updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'retry',
        attempt_count = d.attempt_count + 1,
        next_attempt_at = clock.now + ($5::BIGINT * INTERVAL '1 millisecond'),
        sent_at = NULL,
        last_error = $6,
        last_error_code = $7,
        updated_at = clock.now
    FROM checked CROSS JOIN clock CROSS JOIN passed
    WHERE d.id = checked.id AND passed.value
    RETURNING d.id
), unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET client_request_id = CASE WHEN passed.value THEN $8 ELSE u.client_request_id END,
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
