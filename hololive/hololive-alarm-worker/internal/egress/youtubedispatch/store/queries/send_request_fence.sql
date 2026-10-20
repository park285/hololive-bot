SELECT status = 'SENDING' AND row_version = $2 AND send_request_id = $3 AND locked_by IS NOT DISTINCT FROM $4::text
FROM youtube_notification_delivery WHERE id = $1 FOR UPDATE;
