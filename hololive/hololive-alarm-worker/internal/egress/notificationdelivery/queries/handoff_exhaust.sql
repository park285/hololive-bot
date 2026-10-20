-- 세대나 시도 예산이 없는 Iris failed를 종료 FAILED로 끝낸다. attempt_count + 1, 오류 $5, 저장된 요청에 exhausted를
-- 세워 producer rearm(outbox_enqueue_batch_upsert.sql)이 되살리지 않게 하고 확인을 닫는다.
/* handoff_fence */, updated AS (
    UPDATE notification_delivery_outbox o
    SET status = 'FAILED',
        payload = jsonb_set(o.payload, '{request,exhausted}', 'true'::jsonb),
        attempt_count = o.attempt_count + 1,
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
