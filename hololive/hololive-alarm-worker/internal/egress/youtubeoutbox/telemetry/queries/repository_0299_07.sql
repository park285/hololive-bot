UPDATE youtube_notification_delivery_telemetry
SET locked_at = NULL, next_attempt_at = $1, error = $2
WHERE id = ANY($3::bigint[])
