UPDATE youtube_notification_send_request
SET handoff_request_id = $2,
    handoff_next_check_at = $3::TIMESTAMPTZ + ($4::BIGINT * INTERVAL '1 millisecond'),
    handoff_checked_at = NULL
WHERE base_id = $1
