-- handoff_completed를 확인으로 기록한다. 멤버 delivery·원장은 SENT 그대로이고 요청의 다음 확인을 지우고 확인 시각을
-- 남긴다. 멤버를 바꾸지 않으므로 멤버 fence는 보지 않는다(members_match는 TRUE).
/* handoff_request_fence */, updated AS (
    UPDATE youtube_notification_send_request r
    SET handoff_next_check_at = NULL,
        handoff_checked_at = clock.now
    FROM checked
    CROSS JOIN clock
    WHERE r.base_id = $1
      AND checked.token_matches
      AND checked.request_matches
    RETURNING r.base_id
)
SELECT checked.token_matches, checked.request_matches, TRUE AS members_match,
       EXISTS (SELECT 1 FROM updated) AS applied
FROM checked
