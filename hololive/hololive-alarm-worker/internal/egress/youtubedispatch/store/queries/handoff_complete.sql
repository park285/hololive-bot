/* fragment:handoff_request_fence */, updated AS (
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
