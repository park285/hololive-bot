-- 전이 없이 다음 확인을 clock + $5 밀리초로 예약하되 admission(sent_at) + 지평 $6 밀리초를 넘기지 않는다. 지평
-- 시각의 확인이 마지막 확인이다.
/* handoff_fence */, updated AS (
    UPDATE notification_delivery_outbox o
    SET handoff_next_check_at = LEAST(
            clock.now + ($5::BIGINT * INTERVAL '1 millisecond'),
            passed.sent_at + ($6::BIGINT * INTERVAL '1 millisecond'))
    FROM passed
    CROSS JOIN clock
    WHERE o.id = passed.id
    RETURNING o.id
)
SELECT checked.id, checked.status, checked.status_matches, TRUE AS owner_matches, checked.version_matches,
       TRUE AS lease_valid, TRUE AS membership_matches, TRUE AS members_non_terminal, checked.request_matches,
       checked.id IN (SELECT updated.id FROM updated) AS applied
FROM checked
