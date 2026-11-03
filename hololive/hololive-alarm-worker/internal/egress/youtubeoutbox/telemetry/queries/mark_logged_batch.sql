UPDATE youtube_notification_delivery_telemetry
SET logged_at = $1, locked_at = NULL, error = ''
WHERE id = ANY($2::bigint[])
