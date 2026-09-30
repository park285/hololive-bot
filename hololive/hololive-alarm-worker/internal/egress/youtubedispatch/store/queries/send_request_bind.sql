UPDATE youtube_notification_delivery SET send_request_id = $2
WHERE id = ANY($1::bigint[]) AND send_request_id IS NULL AND status = 'PENDING';
