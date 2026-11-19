UPDATE youtube_notification_send_request
SET generation = generation + 1,
    handoff_request_id = NULL,
    handoff_next_check_at = NULL,
    handoff_checked_at = NULL
WHERE base_id = $1
  AND generation = $2::INT
  AND generation < 2
