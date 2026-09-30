SELECT status = 'SENDING' AND row_version = $2 AND send_request_id = $3
FROM youtube_notification_delivery WHERE id = $1 FOR UPDATE;
