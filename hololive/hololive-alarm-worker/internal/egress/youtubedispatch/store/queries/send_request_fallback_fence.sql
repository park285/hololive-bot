SELECT COALESCE(send_request_id, '') FROM youtube_notification_delivery
WHERE id = $1 AND status = 'SENDING' AND row_version = $2 FOR UPDATE;
