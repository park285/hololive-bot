SELECT r.handoff_request_id, r.generation, r.handoff_next_check_at, r.handoff_checked_at
FROM youtube_notification_send_request r
WHERE r.base_id = $1
