-- handoff_completed를 확인으로 기록한다. 행은 SENT 그대로이고 다음 확인을 지우고 확인 시각을 남긴다. 그때부터 같은
-- 방의 후행 행을 고를 수 있다.
/* handoff_fence */, updated AS (
    UPDATE notification_delivery_outbox o
    SET handoff_next_check_at = NULL,
        handoff_checked_at = clock.now
    FROM passed
    CROSS JOIN clock
    WHERE o.id = passed.id
    RETURNING o.id
)
SELECT checked.id, checked.status, checked.status_matches, TRUE AS owner_matches, checked.version_matches,
       TRUE AS lease_valid, TRUE AS membership_matches, TRUE AS members_non_terminal, checked.request_matches,
       checked.id IN (SELECT updated.id FROM updated) AS applied
FROM checked
