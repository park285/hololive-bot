SELECT id, room_id, status, row_version, attempt_count, COALESCE(send_request_id, '') AS send_request_id, request_snapshot_allowed
FROM youtube_notification_delivery WHERE id = ANY($1::bigint[]) ORDER BY id FOR UPDATE;
