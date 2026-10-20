-- Iris가 dispatch 전에 포기한(failed) 요청을 다음 요청 세대의 PENDING으로 되돌린다. attempt_count + 1,
-- next_attempt_at = clock + $5 밀리초, 오류 $6, sent_at과 handoff 확인 상태는 비운다. 고정 본문·경로·base ID는 그대로라
-- 다음 claim은 같은 본문을 새 세대 ID(base:rN)로 보낸다. 409 CLIENT_REQUEST_ID_FAILED 재발급과 같은 세대·시도 규칙이다.
/* handoff_fence */, updated AS (
    UPDATE notification_delivery_outbox o
    SET status = 'PENDING',
        payload = jsonb_set(o.payload, '{request,generation}', to_jsonb($4::INT + 1)),
        attempt_count = o.attempt_count + 1,
        next_attempt_at = clock.now + ($5::BIGINT * INTERVAL '1 millisecond'),
        error = $6,
        sent_at = NULL,
        handoff_request_id = NULL,
        handoff_next_check_at = NULL,
        handoff_checked_at = NULL
    FROM passed
    CROSS JOIN clock
    WHERE o.id = passed.id
    RETURNING o.id
)
SELECT checked.id, checked.status, checked.status_matches, TRUE AS owner_matches, checked.version_matches,
       TRUE AS lease_valid, TRUE AS membership_matches, TRUE AS members_non_terminal, checked.request_matches,
       checked.id IN (SELECT updated.id FROM updated) AS applied
FROM checked
