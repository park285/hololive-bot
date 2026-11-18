WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
)
UPDATE youtube_notification_send_request r
SET handoff_next_check_at = NULL,
    handoff_checked_at = clock.now
FROM clock
WHERE r.base_id = $1
