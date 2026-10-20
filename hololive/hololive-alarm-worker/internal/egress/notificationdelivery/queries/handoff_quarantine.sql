-- handoff 결과 불명(outcome_unknown·모르는 상태·404·지평 미확인)을 QUARANTINED로 끝내고 확인을 닫는다. 오류 $5를
-- 남기며 자동 재발송 가능한 FAILED로 두지 않는다.
/* handoff_fence */, updated AS (
    UPDATE notification_delivery_outbox o
    SET status = 'QUARANTINED',
        error = $5,
        sent_at = NULL,
        handoff_next_check_at = NULL,
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
